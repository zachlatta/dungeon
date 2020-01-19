package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fabioberger/airtable-go"
	"github.com/joho/godotenv"
	"github.com/nlopes/slack"
)

const CostToPlay = 5 // in GP

// AIRTABLE DB //

type DB struct {
	client *airtable.Client
}

func NewDB(airtableAPIKey, airtableBaseID string) (*DB, error) {
	client, err := airtable.New(airtableAPIKey, airtableBaseID)
	if err != nil {
		return nil, err
	}

	return &DB{
		client: client,
	}, nil
}

type SlackUser struct {
	ID   string
	Name string
}

func (u SlackUser) Eq(ou SlackUser) bool {
	return u.ID == ou.ID
}

func (u SlackUser) ToString() string {
	return u.Name + " <@" + u.ID + ">"
}

var slackUserRegex = regexp.MustCompile("((.+) )?<@([A-Z0-9]+)>")

func SlackUserFromID(client *slack.Client, slackID string) (SlackUser, error) {
	userProfile, err := client.GetUserProfile(slackID, false)
	if err != nil {
		return SlackUser{}, err
	}

	return SlackUser{
		ID:   slackID,
		Name: userProfile.DisplayName,
	}, nil
}

func SlackUserFromString(str string) (SlackUser, error) {
	matches := slackUserRegex.FindStringSubmatch(str)
	if matches == nil {
		return SlackUser{}, errors.New("no slack user matches found")
	}

	return SlackUser{
		Name: matches[2],
		ID:   matches[3],
	}, nil
}

func SlackUsersToString(users []SlackUser) string {
	strs := make([]string, len(users))
	for i, u := range users {
		strs[i] = u.ToString()
	}

	return strings.Join(strs, ", ")
}

func SlackUsersFromIDs(client *slack.Client, ids []string) ([]SlackUser, error) {
	users := make([]SlackUser, len(ids))
	for i, id := range ids {
		var err error
		users[i], err = SlackUserFromID(client, id)
		if err != nil {
			return nil, err
		}
	}

	return users, nil
}

func SlackUsersFromString(str string) ([]SlackUser, error) {
	matches := slackUserRegex.FindAllStringSubmatch(str, -1)
	if matches == nil {
		return nil, errors.New("no slack user matches found")
	}

	fmt.Println(str)
	fmt.Println(matches)

	slackUsers := make([]SlackUser, len(matches))
	for i, match := range matches {
		slackUsers[i] = SlackUser{
			Name: match[2],
			ID:   match[3],
		}
	}

	return slackUsers, nil
}

type Session struct {
	AirtableID      string
	ThreadTimestamp string
	Creator         SlackUser
	Companions      []SlackUser
	CostGP          int
	Paid            bool
	Patron          SlackUser
	Prompt          string
	SessionID       int
}

type airtableSession struct {
	AirtableID string `json:"id,omitempty"`
	Fields     struct {
		ThreadTimestamp string `json:"Thread Timestamp"`
		Creator         string
		Companions      string
		Cost            int  `json:"Cost (GP)"`
		Paid            bool `json:"Paid?"`
		Patron          string
		Prompt          string
		SessionID       int `json:"Session ID,omitempty"`
	} `json:"fields"`
}

func sessionFromAirtable(as airtableSession) (Session, error) {
	creator, err := SlackUserFromString(as.Fields.Creator)
	if err != nil {
		return Session{}, err
	}

	var companions []SlackUser
	if as.Fields.Companions != "" {
		companions, err = SlackUsersFromString(as.Fields.Companions)
		if err != nil {
			return Session{}, err
		}
	}

	var patron SlackUser
	if as.Fields.Patron != "" {
		patron, err = SlackUserFromString(as.Fields.Patron)
		if err != nil {
			return Session{}, err
		}
	}

	return Session{
		AirtableID:      as.AirtableID,
		ThreadTimestamp: as.Fields.ThreadTimestamp,
		Creator:         creator,
		Companions:      companions,
		CostGP:          as.Fields.Cost,
		Paid:            as.Fields.Paid,
		Patron:          patron,
		Prompt:          as.Fields.Prompt,
		SessionID:       as.Fields.SessionID,
	}, nil
}

func (db *DB) CreateSession(threadTs string, creator SlackUser, companions []SlackUser, costGP int, prompt string) (Session, error) {
	as := airtableSession{}
	as.Fields.ThreadTimestamp = threadTs
	as.Fields.Creator = creator.ToString()
	as.Fields.Companions = SlackUsersToString(companions)
	as.Fields.Cost = costGP
	as.Fields.Prompt = prompt

	if err := db.client.CreateRecord("Sessions", &as); err != nil {
		return Session{}, err
	}

	return sessionFromAirtable(as)
}

func (db *DB) GetSession(threadTs string) (Session, error) {
	listParams := airtable.ListParameters{
		// TODO Prevent string escaping problems
		FilterByFormula: `{Thread Timestamp} = "` + threadTs + `"`,
	}

	airtableSessions := []airtableSession{}
	if err := db.client.ListRecords("Sessions", &airtableSessions, listParams); err != nil {
		return Session{}, err
	}

	if len(airtableSessions) > 1 {
		return Session{}, errors.New("too many sessions, non-unique timestamps")
	} else if len(airtableSessions) == 0 {
		return Session{}, errors.New("no session found")
	}

	return sessionFromAirtable(airtableSessions[0])
}

func (db *DB) MarkSessionPaidAndStarted(session Session, patron SlackUser, sessionID int) (Session, error) {
	as := airtableSession{}

	updatedFields := map[string]interface{}{
		"Paid?":      true,
		"Patron":     patron.ToString(),
		"Session ID": sessionID,
	}

	if err := db.client.UpdateRecord("Sessions", session.AirtableID, updatedFields, &as); err != nil {
		return Session{}, err
	}

	return sessionFromAirtable(as)
}

type airtableStoryItem struct {
	AirtableID string `json:"id,omitempty"`
	Fields     struct {
		Session []string
		Type    string
		Author  string
		Value   string
	} `json:"fields"`
}

// author should be nil
func (db *DB) CreateStoryItem(session Session, itemType string, author *SlackUser, value string) error {
	si := airtableStoryItem{}
	si.Fields.Session = []string{session.AirtableID}
	si.Fields.Type = itemType

	if author != nil {
		si.Fields.Author = author.ToString()
	}

	si.Fields.Value = value

	if err := db.client.CreateRecord("Story Items", &si); err != nil {
		return err
	}

	return nil
}

// AI DUNGEON API CLIENT //

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
		Email:     email,
		Password:  password,
		AuthToken: loginResp.AccessToken,
	}, nil
}

func (c AIDungeonClient) CreateSession(prompt string) (sessionId int, output string, err error) {
	body := map[string]interface{}{
		"storyMode":     "custom",
		"characterType": nil,
		"name":          nil,
		"customPrompt":  &prompt,
		"promptId":      nil,
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return 0, "", err
	}

	client := &http.Client{}

	req, err := http.NewRequest("POST", "https://api.aidungeon.io/sessions", bytes.NewBuffer(reqBody))
	if err != nil {
		return 0, "", err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("x-access-token", c.AuthToken)

	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return 0, "", errors.New(fmt.Sprint("http error, status code ", resp.StatusCode))
	}

	type NewSessionResp struct {
		ID    int `json:"id"`
		Story []struct {
			Value string `json:"value"`
		} `json:"story"`
	}

	var newSessionResp NewSessionResp
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&newSessionResp); err != nil {
		return 0, "", err
	}

	if len(newSessionResp.Story) == 0 {
		return 0, "", errors.New("story is empty for some reason")
	}

	return newSessionResp.ID, newSessionResp.Story[0].Value, nil
}

func (c AIDungeonClient) Input(sessionId int, text string) (output string, err error) {
	body := map[string]string{
		"text": text,
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	client := &http.Client{}

	req, err := http.NewRequest("POST", "https://api.aidungeon.io/sessions/"+strconv.Itoa(sessionId)+"/inputs", bytes.NewBuffer(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("x-access-token", c.AuthToken)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		io.Copy(os.Stdout, resp.Body)
		return "", errors.New(fmt.Sprint("http error, status code ", resp.StatusCode))
	}

	type InputResp []struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	}

	var inputResp InputResp
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&inputResp); err != nil {
		return "", err
	}

	if len(inputResp) == 0 {
		return "", errors.New("resp length is zero for some reason...")
	}

	last := inputResp[len(inputResp)-1]
	if last.Type == "input" {
		return "", errors.New("last type is input instead of output...")
	}

	return last.Value, nil
}

// SLACK MESSAGE PARSING //

type Msg interface {
	Raw() *slack.MessageEvent
}

type StartJourneyMsg struct {
	AuthorID        string
	AuthorName      string
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
//     end-of-the-millennium and hatch a plan: you’re going to hack the moon
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
		AuthorID:        m.User,
		ChannelID:       m.Channel,
		ThreadTimestamp: m.Timestamp,
		CompanionIDs:    companionIDs,
		Prompt:          promptText,
		raw:             m,
	}, true
}

type ReceiveMoneyMsg struct {
	AuthorID        string
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

	gpAmount, err := strconv.Atoi(rawGPAmount)
	if err != nil {
		return nil, false
	}

	// check the details, make sure this transfer actually happened for the right amount
	// TODO: figure out better way to work with banker user ID TODO: dynamically get our user ID
	if m.User != "UH50T81A6" || recipientUserID != "USH186XSP" || gpAmount != CostToPlay {
		return nil, false
	}

	return &ReceiveMoneyMsg{
		AuthorID:        m.User,
		RecipientID:     recipientUserID,
		ChannelID:       m.Channel,
		ThreadTimestamp: m.ThreadTimestamp,
		GP:              gpAmount,
		Reason:          reasonGiven,
		raw:             m,
	}, true
}

type InputMsg struct {
	AuthorID        string
	ChannelID       string
	ThreadTimestamp string
	Input           string
	raw             *slack.MessageEvent
}

func (m InputMsg) Raw() *slack.MessageEvent {
	return m.raw
}

func ParseInputMsg(m *slack.MessageEvent) (*InputMsg, bool) {
	// must be in a thread
	if m.ThreadTimestamp == "" {
		return nil, false
	}

	// 2 parts of message: 1. @dungeon and 2. their input for the next step
	// of the session
	regex := regexp.MustCompile(`^<@([A-Z0-9]+)> (.+)$`)
	matches := regex.FindStringSubmatch(m.Text)
	if matches == nil {
		return nil, false
	}

	toUser := matches[1]
	input := matches[2]

	// TODO if m.User != creator of thread (or their buddies, if companions enabled)
	// TODO: dynamically get @dungeon id
	if toUser != "USH186XSP" {
		return nil, false
	}

	return &InputMsg{
		AuthorID:        m.User,
		ChannelID:       m.Channel,
		ThreadTimestamp: m.ThreadTimestamp,
		Input:           input,
		raw:             m,
	}, true
}

func parseMessage(msg *slack.MessageEvent) Msg {
	parsed, ok := ParseStartJourneyMsg(msg)
	if !ok {
		parsed, ok := ParseReceiveMoneyMsg(msg)
		if !ok {
			parsed, ok := ParseInputMsg(msg)
			if !ok {
				return nil
			}

			return parsed
		}

		return parsed
	}

	return parsed
}

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

	aidungeon, err := NewAIDungeonClient(aidungeonEmail, aidungeonPassword)
	if err != nil {
		log.Fatal("error creating aidungeon client:", err)
	}

	log.Println("logged into ai dungeon")

	log.Println("authenticating with airtable")

	db, err := NewDB(airtableAPIKey, airtableBaseID)
	if err != nil {
		log.Fatal("error creating airtable client:", err)
	}

	log.Println("authenticated with airtable")

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

				log.Println("Creating session in Airtable")

				creator, err := SlackUserFromID(api, msg.AuthorID)
				if err != nil {
					// TODO better error handling
					log.Fatal("error creating creator:", err)
				}

				companions, err := SlackUsersFromIDs(api, msg.CompanionIDs)
				if err != nil {
					log.Fatal("error creating companions:", err)
				}

				session, err := db.CreateSession(
					msg.ThreadTimestamp,
					creator,
					companions,
					CostToPlay,
					msg.Prompt,
				)

				log.Println("SESSION CREATED", session)

				rtm.SendMessage(rtm.NewOutgoingMessage(
					"_groggily wakes up..._",
					msg.ChannelID,
					slack.RTMsgOptionTS(msg.ThreadTimestamp),
				))

				time.Sleep(time.Second * 1)

				rtm.SendMessage(rtm.NewOutgoingMessage(
					"Ugh... it's been a while. My bones are rough. My bones are weak. Load me up with "+strconv.Itoa(CostToPlay)+"GP and our journey together will make your week.",
					msg.ChannelID,
					slack.RTMsgOptionTS(msg.ThreadTimestamp),
				))
			case *ReceiveMoneyMsg:
				log.Println("Hoo hah, I got the money:", msg)

				session, err := db.GetSession(msg.ThreadTimestamp)
				if err != nil {
					rtm.SendMessage(rtm.NewOutgoingMessage(
						"Wow, I am truly flattered. Thank you!",
						msg.ChannelID,
						slack.RTMsgOptionTS(msg.ThreadTimestamp),
					))
					log.Println("received money, but unable to find session:", err, "-", msg)

					continue
				}

				if session.Paid {
					rtm.SendMessage(rtm.NewOutgoingMessage(
						"This journey is already paid for, but I'll still happily take your money!",
						msg.ChannelID,
						slack.RTMsgOptionTS(msg.ThreadTimestamp),
					))
					log.Println(
						"received money for already paid session:",
						session.ThreadTimestamp,
						"-",
						msg,
					)

					continue
				}

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

				// indicate we're typing
				rtm.SendMessage(rtm.NewTypingMessage(msg.ChannelID))

				sessionID, output, err := aidungeon.CreateSession(session.Prompt)
				if err != nil {
					log.Fatal("ugh, failed", err)
				}

				// TODO make this actually work
				patron, err := SlackUserFromID(api, msg.AuthorID)
				if err != nil {
					log.Fatal("failed to get deets:", err)
				}

				session, err = db.MarkSessionPaidAndStarted(session, patron, sessionID)
				if err != nil {
					log.Fatal("failed to update airtable record:", err)
				}

				if err := db.CreateStoryItem(session, "Output", nil, output); err != nil {
					log.Fatal("failed to log output:", err)
				}

				rtm.SendMessage(rtm.NewOutgoingMessage(
					"_(remember to @mention me in your replies!)_",
					msg.ChannelID,
					slack.RTMsgOptionTS(msg.ThreadTimestamp),
				))

				// indicate we're typing
				rtm.SendMessage(rtm.NewTypingMessage(msg.ChannelID))

				time.Sleep(time.Second * 1)

				rtm.SendMessage(rtm.NewOutgoingMessage(
					output,
					msg.ChannelID,
					slack.RTMsgOptionTS(msg.ThreadTimestamp),
				))

				fmt.Println("SESSION ID:", sessionID)

			case *InputMsg:
				log.Println("HOO HAH I GOT THE INPUT:", msg)

				session, err := db.GetSession(msg.ThreadTimestamp)
				if err != nil {
					log.Println("input attemped, unable to find session:", err, "-", msg)
					rtm.SendMessage(rtm.NewOutgoingMessage(
						"...I'm sorry. What are you talking about? We're not on a journey together right now.",
						msg.ChannelID,
						slack.RTMsgOptionTS(msg.ThreadTimestamp),
					))

					continue
				}

				author, err := SlackUserFromID(api, msg.AuthorID)
				if err != nil {
					// TODO better errors
					log.Fatal("failed to get slack user info:", err)
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
					rtm.SendMessage(rtm.NewOutgoingMessage(
						"...sorry my friend, but this isn't your journey to embark on.",
						msg.ChannelID,
						slack.RTMsgOptionTS(msg.ThreadTimestamp),
					))

					continue
				}

				if err := db.CreateStoryItem(session, "Input", &author, msg.Input); err != nil {
					log.Fatal("failed to log input:", err)
				}

				// indicate we're typing
				rtm.SendMessage(rtm.NewTypingMessage(msg.ChannelID))

				output, err := aidungeon.Input(session.SessionID, msg.Input)
				if err != nil {
					log.Fatal("ugh, failed", err)
				}

				if err := db.CreateStoryItem(session, "Output", nil, output); err != nil {
					log.Fatal("failed to log output:", err)
				}

				rtm.SendMessage(rtm.NewOutgoingMessage(
					output,
					msg.ChannelID,
					slack.RTMsgOptionTS(msg.ThreadTimestamp),
				))

			default:
				log.Println("unable to parse message event, unknown...")
			}
		}
	}
}
