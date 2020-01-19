package main

import (
	"fmt"
	"log"
	"os"
	"regexp"

	"github.com/joho/godotenv"
	"github.com/nlopes/slack"
)

type Msg interface {
	Raw() *slack.MessageEvent
}

type StartJourneyMsg struct {
	CreatorID    string
	MsgTimestamp string
	CompanionIDs []string
	Prompt       string
	raw          *slack.MessageEvent
}

func (m StartJourneyMsg) Raw() *slack.MessageEvent {
	return m.raw
}

// nil if don't process, value if proceed
//
// Example of messages this funciton will process:
//
//   Without companions:
//
//     <@USH186XSP> You are a lone traveler searching for a wizard in the
//     middle of a gigantic forest. You’ve been searching for days in the
//     forest and are lost. After a rough night’s sleep, you wake up groggy and
//
//   With companions:
//
//     <@USH186XSP> (with <@U0C7B14Q3> and <@UDYDSUDHV>) You are a hacker in
//     the year 2999 in the future-city of Neosporia. While sitting in your
//     high-rise apartment on floor 401, you reflect on the coming
//     end-of-the-millennium and hatch a place: you’re going to hack the moon
//     on New Year’s Eve. You call you friend named
//
func ParseStartJourneyMsg(m *slack.MessageEvent) *StartJourneyMsg {
	// 3 parts of message: 1. our user id, 2. companions, 3. the prompt to
	// start journey with
	regex := regexp.MustCompile(`^<@([A-Z0-9]+)> (\(.*\) )?(.*)$`)
	matches := regex.FindStringSubmatch(m.Text)
	if matches == nil {
		return nil
	}

	myUserID := matches[1]
	companionText := matches[2]
	promptText := matches[3]

	// if this doesn't look like a start journey msg, skip
	// TODO: dynamically get our user ID
	if myUserID != "USH186XSP" || promptText == "" {
		return nil
	}

	// extract companion IDs if present
	slackCompanionIDRegex := regexp.MustCompile(`<@([A-Z0-9]+)>`)
	rawCompanionIDResults :=
		slackCompanionIDRegex.FindAllStringSubmatch(companionText, -1)

	companionIDs := make([]string, len(rawCompanionIDResults))
	for i, companionIDResult := range rawCompanionIDResults {
		companionIDs[i] = companionIDResult[1]
	}

	return &StartJourneyMsg{
		CreatorID:    m.User,
		MsgTimestamp: m.Timestamp,
		CompanionIDs: companionIDs,
		Prompt:       promptText,
		raw:          m,
	}
}

func parseMessage(msg *slack.MessageEvent) Msg {
	return ParseStartJourneyMsg(msg)
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error loading .env file")
	}

	slackAuthToken := os.Getenv("SLACK_LEGACY_TOKEN")

	api := slack.New(slackAuthToken)

	rtm := api.NewRTM()
	go rtm.ManageConnection()

	for rawMsg := range rtm.IncomingEvents {
		log.Println("Event received:", rawMsg)

		switch ev := rawMsg.Data.(type) {
		case *slack.MessageEvent:
			if ev.User == "USLACKBOT" || ev.User == "" {
				continue
			}

			parsed := parseMessage(ev)

			switch msg := parsed.(type) {
			case *StartJourneyMsg:
				fmt.Println("Let's start the journey!", msg)
			}
		}
	}
}
