# Google Cloud setup

Each self-hoster creates their own Google Cloud project. Enable the Gmail API, configure an external OAuth consent screen, and create a **Web application** OAuth client. Add `https://YOUR_DOMAIN/oauth/google/callback` as an authorized redirect URI. Put that client ID and secret in `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`.

The dashboard requests `openid`, `email`, and `https://www.googleapis.com/auth/gmail.send` for each connected Gmail account. `gmail.send` lets the API send mail but not browse the mailbox. Google may show an unverified-app warning for a personal-use app. Set the OAuth app publishing status to **In production** for a long-lived personal-use connection: Google's **Testing** status can make refresh tokens with Gmail scopes expire after seven days. Follow Google's current [scope guide](https://developers.google.com/workspace/gmail/api/auth/scopes), [token lifetime guide](https://developers.google.com/identity/protocols/oauth2), and [personal-use verification guidance](https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification).

The Gmail add-on has its own Apps Script OAuth client identity. Link the Apps Script project to your Cloud project and set `GOOGLE_ADDON_CLIENT_ID` to the audience/client ID of ID tokens returned by `ScriptApp.getIdentityToken()`. The API rejects add-on tokens with a different audience or a Google account that has not been connected in Settings. The add-on asks for compose, draft-recipient metadata, external request, and identity scopes. You must authorize each Gmail account separately.

Keep the OAuth client secret and the `INSTANCE_SECRET` private. Never commit `infra/.env` or `.clasp.json`.
