package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/nlopes/slack"

	"./aidungeon"
	"./db"
)

// MAIN LOGIC //

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error loading .env file")
	}

	slackAuthToken := os.Getenv("SLACK_LEGACY_TOKEN")
	aidungeonEmail := os.Getenv("AIDUNGEON_EMAIL")
	aidungeonPassword := os.Getenv("AIDUNGEON_PASSWORD")
	airtableAPIKey := os.Getenv("AIRTABLE_API_KEY")
	airtableBaseID := os.Getenv("AIRTABLE_BASE")

	log.Println("logging into ai dungeon with email", aidungeonEmail)

	aidungeonc, err := aidungeon.NewClient(aidungeonEmail, aidungeonPassword)
	if err != nil {
		log.Fatal("error connecting to aidungeon:", err)
	}

	log.Println("logged into ai dungeon")

	log.Println("authenticating with airtable")

	dbc, err := db.NewDB(airtableAPIKey, airtableBaseID)
	if err != nil {
		log.Fatal("error connecting with airtable:", err)
	}

	log.Println("authenticated with airtable")

	api := slack.New(slackAuthToken)

	rtm := api.NewRTM()
	go rtm.ManageConnection()

	for rawMsg := range rtm.IncomingEvents {
		log.Println("event received:", rawMsg)

		switch ev := rawMsg.Data.(type) {
		case *slack.MessageEvent:
			// ignore system messages
			if ev.User == "USLACKBOT" || ev.User == "" {
				continue
			}

			log.Println("raw event", ev)

			msg := parseMessage(ev)
			if msg == nil {
				log.Println("unable to parse message event, ignoring...")
				continue
			}

			go msg.Handle(api, rtm, dbc, aidungeonc)
		}
	}
}
