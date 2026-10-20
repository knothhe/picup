package main

import (
	"github.com/knothhe/picup/internal/gui"
	"log"
)

func main() {
	if err := gui.Run(); err != nil {
		log.Fatal(err)
	}
}
