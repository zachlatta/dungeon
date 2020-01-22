package main

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/nlopes/slack"

	"./aidungeon"
	"./db"
)

// CONSTANTS //

const SelfID = "USH186XSP"
const BankerID = "UH50T81A6"
const PlayDungeonChannelID = "CSHEL6LP5"
const CostToPlay = 5 // in GP
const ScenarioIdeas = `here are a few scenario ideas:

• You are King George VII, a noble living in the kingdom of Larion. You have a pouch of gold and a small dagger. You are awakened by one of your servants who tells you that your keep is under attack. You look out the window and see an army of orcs marching towards your capital. They are led by a large orc named

• You are Jenny, a patient living in Chicago. You have a hospital bracelet and a pack of bandages. You wake up in an old rundown hospital with no memory of how you got there. You take a look around the room and see that it is empty except for a bed and some medical equipment. The door to your right leads out into

• You are Ada Lovelace, a courier trying to survive in a post apocalyptic world by scavenging among the ruins of what is left. You have a parcel of letters and a small pistol. It's a long and dangerous road from Boston to Charleston, but you're one of the only people who knows the roads well enough to get your parcel of letters there. You set out in the morning and

• You are Michael Jackson, a pop star and soldier trying to survive in a world filled with infected zombies everywhere. You have an automatic rifle and a grenade. Your unit lost a lot of men when the infection broke, but you've managed to keep the small town you're stationed near safe for now. You look over the town and think about how things could be better, but then you remember that's what soldiers do; they make sacrifices.`

// HELPERS //

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

// SLACK MESSAGE PARSING & HANDLING //

type Msg interface {
	ChannelID() string
	Timestamp() string
	ThreadTimestamp() string
	Raw() *slack.MessageEvent

	// Handle logic associated with the message
	Handle(*slack.Client, *slack.RTM, *db.DB, aidungeon.Client)
}

type StartJourneyMsg struct {
	AuthorID     string
	AuthorName   string
	CompanionIDs []string
	Prompt       string
	raw          *slack.MessageEvent
}

func (m StartJourneyMsg) ChannelID() string {
	return m.raw.Channel
}

func (m StartJourneyMsg) Timestamp() string {
	return m.raw.Timestamp
}

func (m StartJourneyMsg) ThreadTimestamp() string {
	return m.raw.Timestamp
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

	// cannot be in dm
	if strings.HasPrefix(m.Channel, "D") {
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
	if myUserID != SelfID || promptText == "" {
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
		AuthorID:     m.User,
		CompanionIDs: companionIDs,
		Prompt:       promptText,
		raw:          m,
	}, true
}

func (msg StartJourneyMsg) Handle(api *slack.Client, rtm *slack.RTM, dbc *db.DB, aidungeonc aidungeon.Client) {
	log.Println("Let's start the journey!", msg)

	log.Println("Creating session in Airtable")

	creator, err := db.SlackUserFromID(api, msg.AuthorID)
	if err != nil {
		handleSlackError(rtm, msg, err)
		return
	}

	companions, err := db.SlackUsersFromIDs(api, msg.CompanionIDs)
	if err != nil {
		handleSlackError(rtm, msg, err)
		return
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
		return
	}

	log.Println("SESSION CREATED", session)

	threadReply(rtm, msg, "_groggily wakes up..._")

	threadReply(rtm, msg, "Ugh... it's been a while. My bones are rough. My bones are weak. Load me up with "+strconv.Itoa(CostToPlay)+"GP and our journey together will make your week.")
}

type ReceiveMoneyMsg struct {
	AuthorID    string
	RecipientID string
	GP          int
	Reason      string
	raw         *slack.MessageEvent
}

func (m ReceiveMoneyMsg) ChannelID() string {
	return m.raw.Channel
}

func (m ReceiveMoneyMsg) Timestamp() string {
	return m.raw.Timestamp
}

func (m ReceiveMoneyMsg) ThreadTimestamp() string {
	return m.raw.ThreadTimestamp
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

	// make sure this is an actual transfer
	// TODO: figure out better way to work with banker user ID TODO: dynamically get our user ID
	if m.User != BankerID || recipientUserID != SelfID {
		return nil, false
	}

	return &ReceiveMoneyMsg{
		AuthorID:    m.User,
		RecipientID: recipientUserID,
		GP:          gpAmount,
		Reason:      reasonGiven,
		raw:         m,
	}, true
}

func (msg ReceiveMoneyMsg) Handle(api *slack.Client, rtm *slack.RTM, dbc *db.DB, aidungeonc aidungeon.Client) {
	log.Println("Hoo hah, I got the money:", msg)

	session, err := dbc.GetSession(msg.ThreadTimestamp())
	if err != nil {
		log.Println("received money, but unable to find session:", err, "-", msg)
		threadReply(rtm, msg, "Wow, I am truly flattered. Thank you!")
		return
	}

	if session.Paid {
		log.Println("received money for already paid session:", session.ThreadTimestamp, "-", msg)
		threadReply(rtm, msg, "This journey is already paid for, but I'll still happily take your money!")
		return
	}

	if msg.GP < session.CostGP {
		log.Println("received money, but wrong amount. expected", session.CostGP, "but got", msg.GP)
		threadReply(rtm, msg, "Sorry my friend, but that's the wrong amount. Try again.")
		return
	}

	if msg.GP > session.CostGP {
		log.Println("received money greater than expected amount. expected", session.CostGP, "and received", msg.GP)
		threadReply(rtm, msg, strconv.Itoa(msg.GP)+"GP? Wow! That's more than I expected. Let me think on this one...")
	} else if msg.Reason != "" {
		threadReply(rtm, msg, `"`+strings.TrimSpace(msg.Reason)+`", huh? Hope I can live up to that. Let me think on this one...`)
	} else {
		threadReply(rtm, msg, "Ah, now that's a bit better. Let me think on this one...")
	}

	threadReply(rtm, msg, "_:musical_note: elevator music :musical_note:_")

	// indicate we're typing
	rtm.SendMessage(rtm.NewTypingMessage(msg.ChannelID()))

	sessionID, output, err := aidungeonc.CreateSession(session.Prompt)
	if err != nil {
		handleDungeonError(rtm, msg, err)
		return
	}

	session, err = dbc.MarkSessionPaidAndStarted(session, sessionID)
	if err != nil {
		handleDBError(rtm, msg, err)
		return
	}

	if err := dbc.CreateStoryItem(session, "Output", nil, output); err != nil {
		handleDBError(rtm, msg, err)
		return
	}

	threadReply(rtm, msg, "_(remember to @mention me in your replies!)_")

	threadReply(rtm, msg, output)

	log.Println("SESSION ID:", sessionID)
}

type InputMsg struct {
	AuthorID string
	Input    string
	raw      *slack.MessageEvent
}

func (m InputMsg) ChannelID() string {
	return m.raw.Channel
}

func (m InputMsg) Timestamp() string {
	return m.raw.Timestamp
}

func (m InputMsg) ThreadTimestamp() string {
	return m.raw.ThreadTimestamp
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
	if toUser != SelfID {
		return nil, false
	}

	return &InputMsg{
		AuthorID: m.User,
		Input:    input,
		raw:      m,
	}, true
}

func (msg InputMsg) Handle(api *slack.Client, rtm *slack.RTM, dbc *db.DB, aidungeonc aidungeon.Client) {
	log.Println("HOO HAH I GOT THE INPUT:", msg)

	session, err := dbc.GetSession(msg.ThreadTimestamp())
	if err != nil {
		log.Println("input attemped, unable to find session:", err, "-", msg)
		threadReply(rtm, msg, "...I'm sorry. What are you talking about? We're not on a journey together right now.")
		return
	}

	author, err := db.SlackUserFromID(api, msg.AuthorID)
	if err != nil {
		handleDBError(rtm, msg, err)
		return
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
		return
	}

	if err := dbc.CreateStoryItem(session, "Input", &author, msg.Input); err != nil {
		handleDBError(rtm, msg, err)
		return
	}

	// indicate we're typing
	rtm.SendMessage(rtm.NewTypingMessage(msg.ChannelID()))

	output, err := aidungeonc.Input(session.SessionID, msg.Input)
	if err != nil {
		handleDungeonError(rtm, msg, err)
		return
	}

	if err := dbc.CreateStoryItem(session, "Output", nil, output); err != nil {
		handleDBError(rtm, msg, err)
		return
	}

	threadReply(rtm, msg, output)

}

type DMMsg struct {
	AuthorID string
	Text     string
	raw      *slack.MessageEvent
}

func (m DMMsg) ChannelID() string {
	return m.raw.Channel
}

func (m DMMsg) Timestamp() string {
	return m.raw.Timestamp
}

func (m DMMsg) ThreadTimestamp() string {
	return m.raw.ThreadTimestamp
}

func (m DMMsg) Raw() *slack.MessageEvent {
	return m.raw
}

func ParseDMMsg(m *slack.MessageEvent) (*DMMsg, bool) {
	// DMs have channel IDs that start with D
	if strings.HasPrefix(m.Channel, "D") {
		return &DMMsg{
			AuthorID: m.User,
			Text:     m.Text,
			raw:      m,
		}, true
	}

	return nil, false
}

func (msg DMMsg) Handle(api *slack.Client, rtm *slack.RTM, dbc *db.DB, aidungeonc aidungeon.Client) {
	rtm.SendMessage(rtm.NewOutgoingMessage(
		`:wave: hi there! you can only play me in public or private channels (not in DMs). just make sure you invite me (and <@`+BankerID+`>, so you can pay me) into the channel and then give me a prompt. some of the nice folks in slack made <#`+PlayDungeonChannelID+`>, if you want to play me there.

when you give me a prompt, just make sure to @mention my name followed by the scenario you want to start with (ex. `+"`@dungeon The year is 2028 and you are the new president of the United States`"+`). you can even leave an incomplete sentence for me and i'll finish it for you.

`+ScenarioIdeas,
		msg.ChannelID(),
	))
}

// when users just type @dungeon w/o anything else
type MentionMsg struct {
	Text string
	raw  *slack.MessageEvent
}

func (m MentionMsg) ChannelID() string {
	return m.raw.Channel
}

func (m MentionMsg) Timestamp() string {
	return m.raw.Timestamp
}

func (m MentionMsg) ThreadTimestamp() string {
	return m.raw.ThreadTimestamp
}

func (m MentionMsg) Raw() *slack.MessageEvent {
	return m.raw
}

func ParseMentionMsg(m *slack.MessageEvent) (*MentionMsg, bool) {
	if strings.TrimSpace(m.Text) == "<@"+SelfID+">" {
		return &MentionMsg{
			Text: m.Text,
			raw:  m,
		}, true
	}

	return nil, false
}

func (msg MentionMsg) Handle(api *slack.Client, rtm *slack.RTM, dbc *db.DB, aidungeonc aidungeon.Client) {
	err := api.AddReaction("wave", slack.ItemRef{
		Channel:   msg.ChannelID(),
		Timestamp: msg.Timestamp(),
	})
	if err != nil {
		handleSlackError(rtm, msg, err)
		return
	}
}

type HelpMsg struct {
	Text string
	raw  *slack.MessageEvent
}

func (m HelpMsg) ChannelID() string {
	return m.raw.Channel
}

func (m HelpMsg) Timestamp() string {
	return m.raw.Timestamp
}

func (m HelpMsg) ThreadTimestamp() string {
	return m.raw.ThreadTimestamp
}

func (m HelpMsg) Raw() *slack.MessageEvent {
	return m.raw
}

func ParseHelpMsg(m *slack.MessageEvent) (*HelpMsg, bool) {
	if strings.TrimSpace(m.Text) == "<@"+SelfID+">"+" help" {
		return &HelpMsg{
			Text: m.Text,
			raw:  m,
		}, true
	}

	return nil, false
}

func (msg HelpMsg) Handle(api *slack.Client, rtm *slack.RTM, dbc *db.DB, aidungeonc aidungeon.Client) {
	threadReply(rtm, msg,
		`:wave: hi there! together, we can go on _any journey you can possibly imagine_. start me with a prompt (ex. `+"`@dungeon The year is 2028 and you are the new president of the United States`"+`) and i'll generate the rest. you can even start with an incomplete sentence and i'll finish it for you.

once we start a journey together, provide next steps and i'll generate the story (ex. `+"`@dungeon Take out the pistol you've been hiding in your back pocket`"+`). there is no limit to what we can do. your creativity is truly the limit.

`+ScenarioIdeas,
	)
}

// This is the magical, crucial, important function for processing incoming
// Slack messages. It's called from the main message processing logic in main().
//
// Messages must be added to this to be processed.
func parseMessage(msg *slack.MessageEvent) Msg {
	var parsed Msg
	var ok bool

	// alt flows, help / mentions / dms

	parsed, ok = ParseHelpMsg(msg)
	if ok {
		return parsed
	}

	parsed, ok = ParseMentionMsg(msg)
	if ok {
		return parsed
	}

	parsed, ok = ParseDMMsg(msg)
	if ok {
		return parsed
	}

	// main flows, in order flow will happen

	parsed, ok = ParseStartJourneyMsg(msg)
	if ok {
		return parsed
	}

	parsed, ok = ParseReceiveMoneyMsg(msg)
	if ok {
		return parsed
	}

	parsed, ok = ParseInputMsg(msg)
	if ok {
		return parsed
	}

	return nil
}
