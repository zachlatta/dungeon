package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

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

			parsed := parseMessage(ev)

			switch msg := parsed.(type) {
			case *StartJourneyMsg:
				log.Println("Let's start the journey!", msg)

				log.Println("Creating session in Airtable")

				creator, err := db.SlackUserFromID(api, msg.AuthorID)
				if err != nil {
					handleSlackError(rtm, msg, err)
					continue
				}

				companions, err := db.SlackUsersFromIDs(api, msg.CompanionIDs)
				if err != nil {
					handleSlackError(rtm, msg, err)
					continue
				}

				session, err := dbc.CreateSession(
					msg.Timestamp(),
					creator,
					companions,
					CostToPlay,
					msg.Prompt,
				)
				if err != nil {
					handleDBError(rtm, msg, err)
					continue
				}

				log.Println("SESSION CREATED", session)

				threadReply(rtm, msg, "_groggily wakes up..._")

				time.Sleep(time.Second * 1)

				threadReply(rtm, msg, "Ugh... it's been a while. My bones are rough. My bones are weak. Load me up with "+strconv.Itoa(CostToPlay)+"GP and our journey together will make your week.")
			case *ReceiveMoneyMsg:
				log.Println("Hoo hah, I got the money:", msg)

				session, err := dbc.GetSession(msg.ThreadTimestamp())
				if err != nil {
					log.Println("received money, but unable to find session:", err, "-", msg)
					threadReply(rtm, msg, "Wow, I am truly flattered. Thank you!")
					continue
				}

				if session.Paid {
					log.Println("received money for already paid session:", session.ThreadTimestamp, "-", msg)
					threadReply(rtm, msg, "This journey is already paid for, but I'll still happily take your money!")
					continue
				}

				if msg.GP < session.CostGP {
					log.Println("received money, but wrong amount. expected", session.CostGP, "but got", msg.GP)
					threadReply(rtm, msg, "Sorry my friend, but that's the wrong amount. Try again.")
					continue
				}

				if msg.GP > session.CostGP {
					log.Println("received money greater than expected amount. expected", session.CostGP, "and received", msg.GP)
					threadReply(rtm, msg, strconv.Itoa(msg.GP)+"GP? Wow! That's more than I expected. Let me think on this one...")
				} else if msg.Reason != "" {
					threadReply(rtm, msg, `"`+strings.TrimSpace(msg.Reason)+`", huh? Hope I can live up to that. Let me think on this one...`)
				} else {
					threadReply(rtm, msg, "Ah, now that's a bit better. Let me think on this one...")
				}

				time.Sleep(time.Second * 1)

				threadReply(rtm, msg, "_:musical_note: elevator music :musical_note:_")

				// indicate we're typing
				rtm.SendMessage(rtm.NewTypingMessage(msg.ChannelID()))

				sessionID, output, err := aidungeonc.CreateSession(session.Prompt)
				if err != nil {
					handleDungeonError(rtm, msg, err)
					continue
				}

				session, err = dbc.MarkSessionPaidAndStarted(session, sessionID)
				if err != nil {
					handleDBError(rtm, msg, err)
					continue
				}

				if err := dbc.CreateStoryItem(session, "Output", nil, output); err != nil {
					handleDBError(rtm, msg, err)
					continue
				}

				threadReply(rtm, msg, "_(remember to @mention me in your replies!)_")

				threadReply(rtm, msg, output)

				log.Println("SESSION ID:", sessionID)

			case *InputMsg:
				log.Println("HOO HAH I GOT THE INPUT:", msg)

				session, err := dbc.GetSession(msg.ThreadTimestamp())
				if err != nil {
					log.Println("input attemped, unable to find session:", err, "-", msg)
					threadReply(rtm, msg, "...I'm sorry. What are you talking about? We're not on a journey together right now.")
					continue
				}

				author, err := db.SlackUserFromID(api, msg.AuthorID)
				if err != nil {
					handleDBError(rtm, msg, err)
					continue
				}

				authedInput := false
				if session.Creator.Eq(author) {
					authedInput = true
				} else {
					for _, companion := range session.Companions {
						fmt.Println(author, companion, "-", companion.Eq(author))
						if companion.Eq(author) {
							authedInput = true
						}
					}
				}

				if !authedInput {
					log.Println("input attempted from non-creator or contributor:", author.ToString(), "-", msg.Raw())
					threadReply(rtm, msg, "...sorry my friend, but this isn't your journey to embark on.")
					continue
				}

				if err := dbc.CreateStoryItem(session, "Input", &author, msg.Input); err != nil {
					handleDBError(rtm, msg, err)
					continue
				}

				// indicate we're typing
				rtm.SendMessage(rtm.NewTypingMessage(msg.ChannelID()))

				output, err := aidungeonc.Input(session.SessionID, msg.Input)
				if err != nil {
					handleDungeonError(rtm, msg, err)
					continue
				}

				if err := dbc.CreateStoryItem(session, "Output", nil, output); err != nil {
					handleDBError(rtm, msg, err)
					continue
				}

				threadReply(rtm, msg, output)

			case *DMMsg:
				rtm.SendMessage(rtm.NewOutgoingMessage(
					`:wave: hi there! you can only play me in public or private channels (not in DMs). just make sure you invite me (and <@`+"UH50T81A6"+`>, so you can pay me) into the channel and then give me a prompt. some of the nice folks in slack made <#`+"CSHEL6LP5"+`>, if you want to play me there.

when you give me a prompt, just make sure to @mention my name followed by the scenario you want to start with (ex. `+"`@dungeon The year is 2028 and you are the new president of the United States`"+`). you can even leave an incomplete sentence for me and i'll finish it for you.

`+ScenarioIdeas,
					msg.ChannelID(),
				))
			case *MentionMsg:
				err := api.AddReaction("wave", slack.ItemRef{
					Channel:   msg.ChannelID(),
					Timestamp: msg.Timestamp(),
				})
				if err != nil {
					handleSlackError(rtm, msg, err)
					continue
				}
			case *HelpMsg:
				threadReply(rtm, msg,
					`:wave: hi there! together, we can go on _any journey you can possibly imagine_. start me with a prompt (ex. `+"`@dungeon The year is 2028 and you are the new president of the United States`"+`) and i'll generate the rest. you can even start with an incomplete sentence and i'll finish it for you.

once we start a journey together, provide next steps and i'll generate the story (ex. `+"`@dungeon Take out the pistol you've been hiding in your back pocket`"+`). there is no limit to what we can do. your creativity is truly the limit.

`+ScenarioIdeas,
				)
			default:
				log.Println("unable to parse message event, unknown...")
			}
		}
	}
}
