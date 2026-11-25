# Usage statistics

Vutty can count how many people use it, with Google Analytics 4 (GA4). It is **opt-in**: nothing is sent until the user answers **Share** to the question that is asked once after the first unlock, or turns on **Settings → General → Share anonymous usage statistics**.

## What is sent

One `app_open` event each time the main app starts (a terminal window of the same app is not another start), through the GA4 [Measurement Protocol](https://developers.google.com/analytics/devguides/collection/protocol/ga4). The code is `internal/analytics`; the caller is `app/analytics.go`.

| Field | Value |
| --- | --- |
| `client_id` | a random UUID made by the backend when the user says yes, and forgotten when they say no. It is stored in `settings.json` (`analytics_id`), is not part of a preference group, so it is never synced, and a **Reset settings** drops it |
| `app_name` | `vutty` (the constant `analyticsApp`): which of the owner's apps sent it |
| `app_version`, `os`, `arch` | the release, `runtime.GOOS`, `runtime.GOARCH` |
| `engagement_time_msec` | `1`: GA4 counts a user as active only when an event carries engagement time |
| `non_personalized_ads` | `true` |

`TestSendPostsOnlyTheFixedFields` fails when a field is added, so a new field is always a visible, reviewed change; the text the user reads (`AnalyticsFacts`) must be changed with it. Google also receives the IP address of the request, as with any request: the dialog says so.

## What is not covered

- Downloads. GA4 only sees people who opened the app. Download counts are on the GitHub release page (`gh api repos/:owner/:repo/releases`).
- People who said no, who switched off the network, or who run a build without a stream.

## Setting it up: one property, one data stream per app

1. In [Google Analytics](https://analytics.google.com/), create one **property** for all your apps.
2. For each app, **Admin → Data streams → Add stream → Web**. The URL is only a label (for example `https://github.com/vunguyenvu35/vutty`). Copy the **Measurement ID** (`G-XXXXXXXXXX`).
3. In that stream, **Measurement Protocol API secrets → Create**, and copy the secret.
4. Give both to the build of that app (below). Use a different pair for each app, so the numbers stay apart; `app_name` also lets you filter one app inside a stream.

The API secret is not a credential: it is inside every binary, and all it allows is adding events to that stream. Do not reuse it for anything else.

In GA4, **Reports → Realtime** shows an event within seconds and **Reports → Engagement** shows active users per day, week and month. To split by app, version or system, register `app_name`, `app_version`, `os` and `arch` as **custom dimensions** (Admin → Custom definitions, scope *Event*).

## Building with a stream

A build knows its stream only through two linker variables. Without both, `analytics.Enabled()` is false: the app never asks the user and never sends anything (a developer build, a fork).

```sh
export VUTTY_GA_MEASUREMENT_ID=G-XXXXXXXXXX
export VUTTY_GA_API_SECRET=...
make build        # adds -X vutty/internal/analytics.MeasurementID=... and APISecret=... (scripts/analytics-ldflags.sh)
```

For the release workflow, add the two as repository **secrets** with the same names (`VUTTY_GA_MEASUREMENT_ID`, `VUTTY_GA_API_SECRET`). `scripts/analytics-ldflags.sh` refuses a value that does not look like what Google issues, so a stray character cannot become another linker flag. Without the secrets the release is built without a stream.

## Trying it

`VUTTY_ANALYTICS_URL` points the app at another URL instead of Google, such as a local server, as `VUTTY_UPDATE_URL` does for the update check. To see what Google makes of an event, use the validation server, which stores nothing and answers with what is wrong:

```sh
VUTTY_ANALYTICS_URL=https://www.google-analytics.com/debug/mp/collect ./build/bin/vutty
```

## For another app of the same owner

Copy `internal/analytics` as it is (it has no dependency on Vutty besides the module path in the linker flags), pass the app's own `Measurement ID` and secret, and set `analyticsApp` to its name. Reuse the question, the switch and the `analytics_consent` / `analytics_id` settings so every app asks the same way.
