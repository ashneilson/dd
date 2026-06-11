package main

import (
	"flag"
	"log"

	ddapi "github.com/ashneilson/dd/api"
	"github.com/ashneilson/dd/helper"
)

var (
	flagCredentialsPath = flag.String("credentials", "dd-credentials.json", "path to credentials file")
	flagShareCode       = flag.String("code", "", "share code")
	flagPassword        = flag.String("password", "", "password")
	flagPhoneInfo       = flag.String("phone", "API", "phone info to report")
)

func main() {
	flag.Parse()

	if *flagShareCode == "" || *flagPassword == "" {
		log.Fatalf("must specify -code and -password")
	}

	out, err := ddapi.Register(*flagShareCode, *flagPassword, *flagPhoneInfo)
	if err != nil {
		log.Fatalf("can't remoteregister: %v", err)
	}

	if err := helper.SaveCreds(*flagCredentialsPath, out); err != nil {
		log.Fatalf("can't encode and save response: %+v %v", out, err)
	}

	log.Printf("Ok! Saved encrypted credentials at: %v", *flagCredentialsPath)
}
