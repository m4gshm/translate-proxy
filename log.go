package main

import "log"

func logError(err error) {
	log.Printf("ERROR %s\n", err.Error())
}

func logResponse(response []byte) {
	log.Println(string(response))
}

func logDebug(format string, v ...any) {
	log.Printf(format+"\n", v...)
}
