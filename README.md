# @dungeon bot

Slack bot to interface with AI Dungeon.

Setup:

- Create a new Slack user and get a legacy API token from https://api.slack.com/custom-integrations/legacy-tokens

Ideas:

- 5GP to play, modeling usage of making Slack bots to earn GP
- Replies with the dungeon in Slack threads
- Can make the dungeon run you for only, for you + friends, or for anyone to play. Default to for you only. (so people can comment in real-time in the Slack thread)
- Can play via DM (but perhaps make it more expensive to do so?)
- Store data in Airtable

Key flows:

- User mentions @dungeon asking for help
- User mentions @dungeon incorrectly

Other asides:

- @dungeon should invite @banker to the channel if it's not already in it

To-dos:

- [x] Most basic flow working
- [ ] Working DB for multiple sessions

---

AI Dungeon API:

```
POST https://api.aidungeon.io/users

{
  "email": "foo@bar.com",
  "password": "password goes here"
}

Response:

{
  "id": 2339,
  "accessToken":"access token goes here",
  "facebookAccountId":null,
  "facebookAccessToken":null,
  "email":"foo@bar.com",
  "username":"foo",
  "password":"hashed password",
  "gameSafeMode":false,
  "gameShowTips":true,
  "gameTextColor":null,
  "gameTextSpeed":null,
  "isSetup":true,
  "createdAt":"2020-01-02T04:03:36.000Z",
  "updatedAt":"2020-01-02T04:03:49.000Z",
  "deletedAt":null
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
    "type":"output",
    "value":"You are a lone traveler searching for a wizard in the middle of a gigantic forest. You’ve been searching for days in the forest and are lost. After a rough night’s sleep, you wake up groggy and realize that you have no idea where you are or how to get home.\n\nThe only thing that makes sense is the fact that there must be some sort of magical portal somewhere in this forest. If so, it would probably lead to an old abandoned fortress with many traps set around it."
  },
  {
    "type":"input",
    "value":"Jump three times."
  },
  {
    "type":"output",
    "value":"Your feet feel like they're on fire as you jump from branch to branch. The ground looks very soft under your bare feet, but then again it isn’t hard stone."
  }
]
```

```
POST https://api.aidungeon.io/sessions (with invalid x-access-token)

HTTP 401 Unauthorized

"Invalid credentials."
```
