// +build windows

package system

import (
	"fmt"
)

func ReloadTemplatesSig(_ *Application) {
	fmt.Println("Signals are unsupported on Windows.")
}
