package main

import "log"

func logError(err error) {
	log.Printf("ERROR %s\n", err.Error())
}

func logPayload(typ string, payload []byte) {
	log.Println(typ, ":", string(payload))
}

func logDebug(format string, v ...any) {
	log.Printf(format+"\n", v...)
}
