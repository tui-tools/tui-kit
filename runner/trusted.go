package runner

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/tui-tools/tui-kit/internal/trust"
)

// systemDirs are the directories a command that runs as root is looked up in,
// in the order of root's default PATH on the family's distributions. The
// caller's PATH is never consulted for such a command: an ordinary account
// controls its own PATH, and on Fedora ~/.local/bin and ~/bin come first in
// it, so a `firewall-cmd` dropped there would otherwise be the one that runs
// as root after the next confirmed change.
var systemDirs = []string{
	"/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin",
}

// Hooks the tests replace: which directories are trusted, how a found file is
// checked, and who the process runs as.
var (
	trustedDirs   = func() []string { return systemDirs }
	verifyTrusted = trust.CheckFile
	geteuid       = os.Geteuid
)

// lookTrusted resolves a binary for a command that runs as root: in the
// system directories, then in the tool's own SearchPaths, and never on PATH.
// The file found must be root's, and so must every directory above it (see
// internal/trust). One that is not stops the lookup with an error rather than
// falling through to the next candidate: a user-writable binary sitting where
// an administrative tool lives is something to report, not to step around.
func lookTrusted(bin string, searchPaths []string, hint string) (string, error) {
	var candidates []string
	if strings.ContainsRune(bin, '/') {
		candidates = []string{bin}
	} else {
		for _, dir := range trustedDirs() {
			candidates = append(candidates, filepath.Join(dir, bin))
		}
		candidates = append(candidates, searchPaths...)
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		if err := verifyTrusted(candidate); err != nil {
			return "", fmt.Errorf("%w: refusing %s for a command run as root: %v",
				ErrNotAvailable, candidate, err)
		}
		return candidate, nil
	}
	message := fmt.Sprintf("the %s command was not found in %s", bin,
		strings.Join(trustedDirs(), ", "))
	if onPath, err := exec.LookPath(bin); err == nil {
		message += fmt.Sprintf(" (%s is on PATH, but PATH is not used for a "+
			"command run as root)", onPath)
	}
	if hint != "" {
		message += "; " + hint
	}
	return "", fmt.Errorf("%w: %s", ErrNotAvailable, message)
}

// keptVariables are the only variables of the caller's environment a command
// run as root inherits. TZ keeps timestamps in the zone the user reads them
// in; nothing else a privileged command needs comes from the caller.
var keptVariables = []string{"TZ"}

// trustedEnv is the environment a command run as root starts from: a PATH of
// the system directories, root's own HOME when the process is root, and the
// kept variables. Everything else the caller had (a PATH with ~/.local/bin in
// it, LD_PRELOAD, a HOME pointing at the user's dotfiles under `sudo -E`) is
// left behind. sudo resets the environment on its own as well, but not under
// `sudo -E` or a policy that keeps variables, and not at all when the tool
// itself was started as root.
func trustedEnv(root bool) []string {
	env := []string{"PATH=" + strings.Join(trustedDirs(), ":")}
	if root {
		env = append(env, "HOME="+rootHome())
	}
	for _, name := range keptVariables {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// rootHome is the home directory of the effective user from the account
// database, never from $HOME, which the caller sets.
func rootHome() string {
	if u, err := user.LookupId(fmt.Sprint(geteuid())); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return "/root"
}
