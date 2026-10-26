package main

import (
	"github.com/knothhe/picup/internal/devmaintenance"
	"github.com/knothhe/picup/internal/gui"
	"log"
	"os"
)

func main() {
	if handled, err := devmaintenance.Run(os.Args[1:], os.Stdout); handled {
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := gui.Run(); err != nil {
		log.Fatal(err)
	}
}
