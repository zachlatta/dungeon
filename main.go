package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/nlopes/slack"
)

type AIDungeonClient struct {
	Email     string
	Password  string
	AuthToken string
}

func NewAIDungeonClient(email, password string) (AIDungeonClient, error) {
	body := map[string]string{
		"email":    email,
		"password": password,
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return AIDungeonClient{}, nil
	}

	resp, err := http.Post("https://api.aidungeon.io/users", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return AIDungeonClient{}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return AIDungeonClient{}, errors.New(fmt.Sprint("http error, status code ", resp.StatusCode))
	}

	type LoginResp struct {
		AccessToken string `json:"accessToken"`
	}

	var loginResp LoginResp
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&loginResp); err != nil {
		return AIDungeonClient{}, nil
	}

	return AIDungeonClient{
		Email:    email,
		Password: password,
	}, nil
}

func (c AIDungeonClient) CreateSession(prompt string) (sessionId, output string, err error) {
	return "", "", nil
}

func (c AIDungeonClient) Input(sessionId, text string) (output string, err error) {
	return "", nil
}

type Msg interface {
	Raw() *slack.MessageEvent
}

type StartJourneyMsg struct {
	CreatorID       string
	ChannelID       string
	ThreadTimestamp string
	CompanionIDs    []string
	Prompt          string
	raw             *slack.MessageEvent
}

func (m StartJourneyMsg) Raw() *slack.MessageEvent {
	return m.raw
}

// nil if don't process, value if proceed
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
func ParseStartJourneyMsg(m *slack.MessageEvent) (*StartJourneyMsg, bool) {
	// cannot be in a thread
	if m.ThreadTimestamp != "" {
		return nil, false
	}

	// 3 parts of message: 1. our user id, 2. companions, 3. the prompt to
	// start journey with
	regex := regexp.MustCompile(`^<@([A-Z0-9]+)> (\(.*\) )?(.*)$`)
	matches := regex.FindStringSubmatch(m.Text)
	if matches == nil {
		return nil, false
	}

	myUserID := matches[1]
	companionText := matches[2]
	promptText := matches[3]

	// if this doesn't look like a start journey msg, skip
	// TODO: dynamically get our user ID
	if myUserID != "USH186XSP" || promptText == "" {
		return nil, false
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
		CreatorID:       m.User,
		ChannelID:       m.Channel,
		ThreadTimestamp: m.Timestamp,
		CompanionIDs:    companionIDs,
		Prompt:          promptText,
		raw:             m,
	}, true
}

type ReceiveMoneyMsg struct {
	CreatorID       string
	RecipientID     string
	ChannelID       string
	ThreadTimestamp string
	GP              int
	Reason          string
	raw             *slack.MessageEvent
}

func (m ReceiveMoneyMsg) Raw() *slack.MessageEvent {
	return m.raw
}

func ParseReceiveMoneyMsg(m *slack.MessageEvent) (*ReceiveMoneyMsg, bool) {
	// must be in a thread
	if m.ThreadTimestamp == "" {
		return nil, false
	}

	// 3 parts of message: 1. GP amount, 2. GP receipient ID, 3. (optional)
	// the reason the user gave to banker for the transfer
	regex := regexp.MustCompile(`^I shall transfer ([0-9,]+)gp to <@([A-Z0-9]+)> immediately( for "(.*)")?.*$`)
	matches := regex.FindStringSubmatch(m.Text)
	if matches == nil {
		return nil, false
	}

	rawGPAmount := matches[1]
	recipientUserID := matches[2]
	reasonGiven := matches[4]

	fmt.Println("GP Amount:", rawGPAmount)
	fmt.Println("Recipient user ID:", recipientUserID)
	fmt.Println("Reason given:", reasonGiven)

	gpAmount, err := strconv.Atoi(rawGPAmount)
	if err != nil {
		return nil, false
	}

	// check the details, make sure this transfer actually happened for the right amount
	// TODO: figure out better way to work with banker user ID TODO: dynamically get our user ID
	if m.User != "UH50T81A6" || recipientUserID != "USH186XSP" || gpAmount != 5 {
		return nil, false
	}

	return &ReceiveMoneyMsg{
		CreatorID:       m.User,
		RecipientID:     recipientUserID,
		ChannelID:       m.Channel,
		ThreadTimestamp: m.ThreadTimestamp,
		GP:              gpAmount,
		Reason:          reasonGiven,
		raw:             m,
	}, true
}

func parseMessage(msg *slack.MessageEvent) Msg {
	parsed, ok := ParseStartJourneyMsg(msg)
	if !ok {
		parsed, ok := ParseReceiveMoneyMsg(msg)
		if !ok {
			return nil
		}

		return parsed
	}

	return parsed
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error loading .env file")
	}

	slackAuthToken := os.Getenv("SLACK_LEGACY_TOKEN")
	aidungeonEmail := os.Getenv("AIDUNGEON_EMAIL")
	aidungeonPassword := os.Getenv("AIDUNGEON_PASSWORD")

	_, err = NewAIDungeonClient(aidungeonEmail, aidungeonPassword)
	if err != nil {
		log.Fatal("error creating aidungeon client:", err)
	}

	log.Println("logged into ai dungeon with email", aidungeonEmail)

	api := slack.New(slackAuthToken)

	rtm := api.NewRTM()
	go rtm.ManageConnection()

	for rawMsg := range rtm.IncomingEvents {
		log.Println("event received:", rawMsg)

		switch ev := rawMsg.Data.(type) {
		case *slack.MessageEvent:
			if ev.User == "USLACKBOT" || ev.User == "" {
				continue
			}

			log.Println("raw event", ev)

			parsed := parseMessage(ev)

			switch msg := parsed.(type) {
			case *StartJourneyMsg:
				log.Println("Let's start the journey!", msg)

				rtm.SendMessage(rtm.NewOutgoingMessage(
					"_groggily wakes up..._",
					msg.ChannelID,
					slack.RTMsgOptionTS(msg.ThreadTimestamp),
				))

				time.Sleep(time.Second * 1)

				rtm.SendMessage(rtm.NewOutgoingMessage(
					"Ugh... it's been a while. My bones are rough. My bones are weak. Load me up with 5GP and our journey together will make your week.",
					msg.ChannelID,
					slack.RTMsgOptionTS(msg.ThreadTimestamp),
				))
			case *ReceiveMoneyMsg:
				log.Println("Hoo hah, I got the money:", msg)

				if msg.Reason != "" {
					rtm.SendMessage(rtm.NewOutgoingMessage(
						`"`+strings.TrimSpace(msg.Reason)+`", huh? Hope I can live up to that. Let me think on this one...`,
						msg.ChannelID,
						slack.RTMsgOptionTS(msg.ThreadTimestamp),
					))
				} else {
					rtm.SendMessage(rtm.NewOutgoingMessage(
						"Ah, now that's a bit better. Let me think on this one...",
						msg.ChannelID,
						slack.RTMsgOptionTS(msg.ThreadTimestamp),
					))
				}

				time.Sleep(time.Second * 1)

				rtm.SendMessage(rtm.NewOutgoingMessage(
					"_:musical_note: elevator music :musical_note:_",
					msg.ChannelID,
					slack.RTMsgOptionTS(msg.ThreadTimestamp),
				))
			default:
				log.Println("unable to parse message event, unknown...")
			}
		}
	}
}
