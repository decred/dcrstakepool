package system

import (
	"os"
	"os/signal"
)

func reloadTemplatesSig(sig os.Signal, app *Application) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, sig)

	go func() {
		for {
			sigr := <-sigChan
			log.Infof("Received: %s", sig)
			if sigr == sig {
				err := app.LoadTemplates(app.TemplatesPath)
				log.Infof("LoadTemplates() executed.")
				if err != nil {
					log.Errorf("LoadTemplates failed: %v", err)
				}
			}
		}
	}()
}
