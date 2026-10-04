# Install the Gmail add-on on your own accounts

The add-on is installed from your own Apps Script project. Its manifest must name your instance's HTTPS domain. Gmail does not offer a generic shared add-on that may call arbitrary self-hosted domains.

1. Install the repository dependencies with `pnpm install`, then run `pnpm --filter gmail-addon configure https://YOUR_DOMAIN`. This writes ignored `src/Config.gs` and `src/appsscript.json` for your instance.
2. In `apps/gmail-addon`, run `pnpm exec clasp login`, then `pnpm exec clasp create-script --title "Mail Tracker" --rootDir src`. Enable the Apps Script API in your Google account if clasp prompts. Run `pnpm exec clasp push` and `pnpm exec clasp open-script`.
3. In the Apps Script project's settings, link it to the Cloud project from [Google Cloud setup](google-cloud.md). Configure the same consent screen for the add-on's scopes and set `GOOGLE_ADDON_CLIENT_ID` in `infra/.env` to the Apps Script OAuth client's ID token audience. Restart the API after changing the environment file.
4. In Apps Script, make a **test deployment** of the Google Workspace add-on and install it for your Gmail account. For another Gmail account, authorize and install it there too. Google’s [test deployment guide](https://developers.google.com/workspace/add-ons/how-tos/testing-workspace-addons) shows the current console flow. A versioned deployment also needs the configured URL allowlist, which the generator writes.
5. Connect each Gmail address in dashboard Settings. Click **Pair add-on** for that mailbox, copy the one-use code, and enter it in the add-on's home card while signed into the matching Gmail account. Codes expire after 10 minutes.
6. In a Gmail compose window, use the add-on's **Prepare tracking** action. Enter a subject for the dashboard and click **Insert tracking image**. The image is inserted at the end of the draft. The dashboard shows **Prepared**. A recipient image request changes the open-detection state; the add-on cannot confirm sending.

The image is 1×1 and styled `display:none`. Mail clients may rewrite or drop its style, remove the image, or cache it. Test a real sent draft to a recipient account you control before relying on it. The add-on's draft path is aggregate when there are multiple recipients. Use the dashboard's Separate sends for individual pixels.

## Manually insert a tracked link

The compose card accepts an optional HTTP(S) destination and label. Insert tracking creates a link tied to that prepared conversation and appends it with the image. Existing links in the Gmail draft are not automatically rewritten. After updating the source, save/reinstall the test deployment or publish a new add-on version as appropriate. Gmail send confirmation remains unavailable in add-on mode until separately reconciled.
