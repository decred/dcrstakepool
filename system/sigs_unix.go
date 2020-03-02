// +build darwin dragonfly freebsd linux nacl netbsd openbsd solaris

package system

import "syscall"

// ReloadTemplatesSig forces the html templates to be reloaded by signalling
// SIGUSR1.
func ReloadTemplatesSig(app *Application) {
	reloadTemplatesSig(syscall.SIGUSR1, app)
}
