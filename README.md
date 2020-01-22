# @dungeon

<img src="https://zachinto2020.files.wordpress.com/2020/01/dungeon_logo.png" width="130" alt="Dungeon Logo" align="right">

👋 Hi there! Together, we can go on _any journey you can possibly imagine_. Start me with a prompt (ex. `@dungeon The year is 2028 and you are the new president of the  United States`) and I'll generate the rest. You can even start with an incomplete sentence and I'll finish it for you.

Once we start a journey together, provide next steps and I'll generate the story (ex. `@dungeon Take out the pistol you've been hiding in your back pocket`). There is no limit to what we can do.

If you want to see what I'm capable of, go on the [Hack Club Slack](https://slack.hackclub.com) into [#playdungeon](https://app.slack.com/client/T0266FRGM/CSHEL6LP5) to see some of the journeys the community has gone on. With me, your creativity is truly the limit.

![Dungeon Demo](https://zachinto2020.files.wordpress.com/2020/01/dungeon_demo_optimized.gif)

#### Setup

- Create a new Slack user and get a legacy API token from https://api.slack.com/custom-integrations/legacy-tokens. Set as `SLACK_LEGACY_TOKEN` in your environment.
- Create AI Dungeon user. Set `AIDUNGEON_EMAIL` and `AIDUNGEON_PASSWORD` in your environment.
- Create an Airtable base that adheres to schema (see `db/db.go` to figure out schema) and set `AIRTABLE_API_KEY` and `AIRTABLE_BASE` in your environment.
- Go into `msgs.go` and update constants for your Slack setup.
- Build and run it! `$ go build && ./dungeon`

#### Ideas during creation

- 5GP to play, modeling usage of making Slack bots to earn GP
- Replies with the dungeon in Slack threads
- Can make `@dungeon` run you for only, for you + friends, or for anyone to play. Default to for you only. (so people can comment in real-time in the Slack thread)
- Store data in Airtable

#### ...so is this just a Slack client for AI Dungeon?

Yeah. And here are the important parts of AI Dungeon's API:

```
POST https://api.aidungeon.io/users


{
  "email": "foo@bar.com",
  "password": "password goes here"
}

Response:

{
  "id": 2339,
  "accessToken": "access token goes here",
  "facebookAccountId": null,
  "facebookAccessToken": null,
  "email": "foo@bar.com",
  "username": "foo",
  "password": "hashed password",
  "gameSafeMode": false,
  "gameShowTips": true,
  "gameTextColor": null,
  "gameTextSpeed": null,
  "isSetup": true,
  "createdAt": "2020-01-02T04:03:36.000Z",
  "updatedAt": "2020-01-02T04:03:49.000Z",
  "deletedAt": null
}
```

```
POST https://api.aidungeon.io/sessions

x-access-token: access-token-goes-here

{
  "storyMode": "custom",
  "characterType": null,
  "name": null,
  "customPrompt": "You are a lone traveler searching for a wizard in the middle of a gigantic forest. You’ve been searching for days in the forest and are lost. After a rough night’s sleep, you wake up groggy and",
  "promptId": null
}

Response:

{
  "visibility": "unpublished",
  "id": 3381,
  "promptId": null,
  "userId": 2339,
  "story": [
    {
      "type": "output",
      "value": "You are a lone traveler searching for a wizard in the middle of a gigantic forest. You’ve been searching for days in the forest and are lost. After a rough night’s sleep, you wake up groggy and realize that you have no idea where you are or how to get home.\n\nThe only thing that makes sense is the fact that there must be some sort of magical portal somewhere in this forest. If so, it would probably lead to an old abandoned fortress with many traps set around it."
    }
  ],
  "context": [],
  "updatedAt": "2020-01-18T03:32:07.290Z",
  "createdAt": "2020-01-18T03:32:07.288Z",
  "publicId": "x3KAlwXl"
}
```

```
POST https://api.aidungeon.io/sessions/:session_id/inputs

x-access-token: access-token-goes-here

{
  "text": "Jump three times."
}

Response:

[
  {
    "type": "output",
    "value": "You are a lone traveler searching for a wizard in the middle of a gigantic forest. You’ve been searching for days in the forest and are lost. After a rough night’s sleep, you wake up groggy and realize that you have no idea where you are or how to get home.\n\nThe only thing that makes sense is the fact that there must be some sort of magical portal somewhere in this forest. If so, it would probably lead to an old abandoned fortress with many traps set around it."
  },
  {
    "type": "input",
    "value": "Jump three times."
  },
  {
    "type": "output",
    "value": "Your feet feel like they're on fire as you jump from branch to branch. The ground looks very soft under your bare feet, but then again it isn’t hard stone."
  }
]
```

```
POST https://api.aidungeon.io/sessions (with invalid x-access-token)

HTTP 401 Unauthorized

"Invalid credentials."
```
