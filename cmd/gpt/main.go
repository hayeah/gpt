package main

import (
	"github.com/hayeah/gpt"
)

func main() {
	app, err := gpt.InitApp()
	if err != nil {
		panic(err)
	}

	err = app.Run()
	if err != nil {
		panic(err)
	}
}
