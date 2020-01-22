package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/nlopes/slack"

	"./aidungeon"
	"./db"
)

const CostToPlay = 5 // in GP

const ScenarioIdeas = `here are a few scenario ideas:

• You are King George VII, a noble living in the kingdom of Larion. You have a pouch of gold and a small dagger. You are awakened by one of your servants who tells you that your keep is under attack. You look out the window and see an army of orcs marching towards your capital. They are led by a large orc named

• You are Jenny, a patient living in Chicago. You have a hospital bracelet and a pack of bandages. You wake up in an old rundown hospital with no memory of how you got there. You take a look around the room and see that it is empty except for a bed and some medical equipment. The door to your right leads out into

• You are Ada Lovelace, a courier trying to survive in a post apocalyptic world by scavenging among the ruins of what is left. You have a parcel of letters and a small pistol. It's a long and dangerous road from Boston to Charleston, but you're one of the only people who knows the roads well enough to get your parcel of letters there. You set out in the morning and

• You are Michael Jackson, a pop star and soldier trying to survive in a world filled with infected zombies everywhere. You have an automatic rifle and a grenade. Your unit lost a lot of men when the infection broke, but you've managed to keep the small town you're stationed near safe for now. You look over the town and think about how things could be better, but then you remember that's what soldiers do; they make sacrifices.`

// MAIN LOGIC //

func threadReply(rtm *slack.RTM, msg Msg, text string) {
	rtm.SendMessage(rtm.NewOutgoingMessage(
		text,
		msg.ChannelID(),
		slack.RTMsgOptionTS(msg.ThreadTimestamp()),
	))
}

func handleSlackError(rtm *slack.RTM, msg Msg, err error) {
	log.Println("slack api error:", err)
	threadReply(rtm, msg, "Sorry, I'm having trouble connecting to Slack. Try again? (slack error)")
}

func handleDBError(rtm *slack.RTM, msg Msg, err error) {
	log.Println("airtable api error:", err)
	threadReply(rtm, msg, "Gosh, I'm having trouble remembering things right now. Sorry about that. Try again in a bit? (db error)")
}

func handleDungeonError(rtm *slack.RTM, msg Msg, err error) {
	log.Println("ai dungeon api error:", err)
	threadReply(rtm, msg, "Gosh, I'm having trouble thinking about our journey right now. Sorry about that. Try again in a bit? (backend error)")
}

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
