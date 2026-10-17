# Runs scripts/*.ax against the stand-ins of ng.be (started by run.be).
import global

def counter_with(hub)
  global.counter = load("scripts/counter.ax", {"hub_url": hub, "text_color": 0xFFFFFF,
    "up_color": 0x00FF66, "down_color": 0xFF3333, "bar_color": 0x00A2FF, "party_icon": "9554"})
end

def tick(app)   # one loop() that fetches (ticks at 0)
  app.ticks = 0
  app.loop()
end

def texts(app)  # the pieces as plain text, e.g. "794 +7"
  if app.parts == nil return nil end
  var s = ""
  for p : app.parts s += p[0] end
  return s
end

# --- parity with the Go hub: testdata/parity.json, also read by internal/*/parity_test.go ---
import json
var pf = open("testdata/parity.json")
var parity = json.load(pf.read())
pf.close()

counter_with("")
var c = counter.Counter(global.Store({}), counter.cfg, "k", "K")
for tc : parity["count"]
  check("count " + str(tc[0]), c.count(tc[0]), tc[1])
end
for d : parity["known_divergences"]["count"]   # documented: once fixed, move them to "count"
  check("known divergence " + str(d["value"]), c.count(d["value"]), d["berry"])
end
check("today", c.today(), 20261016)

# delta and bar, as render() builds them from the stored state
def rendered(step, v, day_start)
  var app = counter.Counter(global.Store({"step": step}), counter.cfg, "k", "K")
  app.st.set("state", {"v": v, "at": app.now(), "d": app.today(), "s": day_start, "c": -1})
  app.render()
  return app
end
for tc : parity["delta"]
  var a = rendered(0, 100000000, 100000000 - tc[0])
  var piece = size(a.parts) > 1 ? a.parts[1][0] : ""
  check("delta " + str(tc[0]), piece, tc[1] == "" ? "" : " " + tc[1])
end
for tc : parity["progress"]
  var a = rendered(tc["step"], tc["value"], tc["value"])
  check("progress " + str(tc["value"]) + "/" + str(tc["step"]), a.pct, tc["pct"] == nil ? -1 : tc["pct"])
end

# parties: the same readings celebrate the same milestones
for tc : parity["milestones"]
  global.notes = []
  var a = counter.Counter(global.Store({"step": tc["step"]}), counter.cfg, "k", "K")
  var got = []
  for v : tc["readings"]
    var before = size(notes)
    a.got(v, "test")
    got.push(size(notes) > before ? a.st.get("state")["c"] : 0)
  end
  check("parties step " + str(tc["step"]), got, tc["party"])
end
global.notes = []

# --- GitHub direct: baseline, then a new day with a milestone ---
var gh = load("scripts/github.ax", {"user": "marco", "token": "", "icon": "21802", "step": 100, "interval_min": 30, "source": "auto"})
check("nothing before the first answer", gh.should_show(), false)
tick(gh)
var r = http.answer('{"login":"marco","followers": 794,"following":3}', 200)
check("github url", r["url"], "https://api.github.com/users/marco")
check("no auth without token", r["opts"]["headers"].find("Authorization"), nil)
check("first reading", texts(gh), "794")
check("progress", gh.pct, 94)
check("baseline does not celebrate", size(notes), 0)
gh.draw()
check("draw", frame, [["icon", "21802"], ["text", [["794", 0xFFFFFF]]], ["progress", 94]])

T["d"] = 17
tick(gh)
http.answer('{"followers": 801}', 200)
check("delta of the new day", texts(gh), "801 +7")
check("milestone party", size(notes), 1)
check("party text", notes[0]["text"], "GITHUB 800!")
check("party sound", notes[0]["sound"]["rtttl"], "party:d=16,o=5,b=180:c,e,g,c6,8p,g,2c6")

tick(gh)
http.answer('{"followers": 799}', 200)
check("drop", texts(gh), "799 +5")
check("no second party below the milestone", size(notes), 1)

tick(gh)
http.answer(nil, 0)
check("network failure keeps the value", texts(gh), "799 +5")
check("retry soon", gh.ticks, 60)
var waits = []
for i : 1 .. 5
  tick(gh)
  http.answer(nil, 0)
  waits.push(gh.ticks / 60)
end
check("backoff doubles up to the interval", waits, [2, 4, 8, 16, 30])
tick(gh)
http.answer('{"followers": 799}', 200)
check("success resets the backoff", gh.fails, 0)
tick(gh)
http.answer(nil, 0)
check("first failure after a success", gh.ticks, 60)
tick(gh)
http.answer('{"message":"API rate limit exceeded"}', 403)
check("rate limit waits the whole interval", gh.ticks, 1800)
tick(gh)
http.answer('{"followers": 799}', 200)

T["epoch"] += 7 * 3600
gh.render()
check("gray when old", gh.parts[0][1], 0x666666)

# --- hub first, fallback to direct in the same cycle ---
global.notes = []
counter_with("http://hub:7080")
var gh2 = load("scripts/github.ax", {"user": "marco", "token": "t0k", "icon": "21802", "step": 100, "interval_min": 30, "source": "auto"})
tick(gh2)
r = http.answer('{"age":60,"value":810}', 200)
check("hub url", r["url"], "http://hub:7080/api/v1/metrics/github_followers")
check("value from hub", texts(gh2), "810")
check("nothing else queued", size(http.queue), 0)

tick(gh2)
http.answer(nil, 0)                       # hub is down
check("direct right after the hub failed", size(http.queue), 1)
r = http.answer('{"followers": 811}', 200)
check("token sent", r["opts"]["headers"]["Authorization"], "Bearer t0k")
check("value from direct", texts(gh2), "811 +1")

tick(gh2)
check("hub skipped while down", http.queue[0]["url"], "https://api.github.com/users/marco")
http.answer('{"followers": 812}', 200)

T["mono"] += 31 * 60 * 1000
tick(gh2)
check("hub asked again after 30 min", http.queue[0]["url"], "http://hub:7080/api/v1/metrics/github_followers")
http.answer('{"age":90000,"value":1}', 200)   # too old: treated as down
check("old hub value falls back to direct", http.queue[0]["url"], "https://api.github.com/users/marco")
http.answer('{"followers": 812}', 200)

# --- source=hub never fetches direct ---
var gh3 = load("scripts/github.ax", {"user": "marco", "token": "", "icon": "21802", "step": 100, "interval_min": 30, "source": "hub"})
tick(gh3)
http.answer(nil, 0)
check("hub only: no direct fetch", size(http.queue), 0)
check("hub only: nothing to show", gh3.should_show(), false)

# --- Stars: GraphQL with token, nothing direct without it ---
counter_with("")
var st = load("scripts/github_stars.ax", {"user": "marco", "token": "", "icon": "gh_stars", "step": 50, "interval_min": 30, "source": "auto"})
tick(st)
check("stars: no token, no request", size(http.queue), 0)
st = load("scripts/github_stars.ax", {"user": "marco", "token": "abc", "icon": "gh_stars", "step": 50, "interval_min": 30, "source": "auto"})
tick(st)
r = http.answer('{"data":{"user":{"repositories":{"nodes":[{"stargazerCount":0},{"stargazerCount":7},{"stargazerCount":270}]}}}}', 200)
check("graphql method", r["m"], "POST")
check("graphql body", r["body"], '{"query":"{user(login:\\"marco\\"){repositories(ownerAffiliations:OWNER,isFork:false,privacy:PUBLIC,first:100){nodes{stargazerCount}}}}"}')
check("stars sum", texts(st), "277")

# --- YouTube ---
var yt = load("scripts/youtube.ax", {"handle": "omarcodev", "api_key": "K", "icon": "youtube", "step": 100, "interval_min": 15, "source": "auto"})
tick(yt)
r = http.answer('{\n  "statistics": {\n    "viewCount": "5759",\n    "subscriberCount": "148",\n    "hiddenSubscriberCount": false\n  }\n}', 200)
check("youtube url", r["url"], "https://www.googleapis.com/youtube/v3/channels?part=statistics&forHandle=omarcodev&key=K")
check("subscribers", texts(yt), "148")

# --- Instagram: seed, weekly refresh, invalid token alert ---
global.notes = []
var ig = load("scripts/instagram.ax", {"token": "seed1", "graph_version": "v26.0", "icon": "36659", "step": 500, "interval_min": 30, "source": "auto"})
tick(ig)
r = http.answer('{"followers_count":4475,"id":"1"}', 200)
check("instagram url", r["url"], "https://graph.instagram.com/v26.0/me?fields=followers_count&access_token=seed1")
check("followers", texts(ig), "4475")

T["epoch"] += 8 * 86400
tick(ig)
r = http.answer('{"access_token":"fresh2","token_type":"bearer","expires_in":5184000}', 200)
check("refresh url", r["url"], "https://graph.instagram.com/refresh_access_token?grant_type=ig_refresh_token&access_token=seed1")
r = http.answer('{"followers_count":4480}', 200)
check("uses the refreshed token", r["url"], "https://graph.instagram.com/v26.0/me?fields=followers_count&access_token=fresh2")

tick(ig)
http.answer('{"error":{"message":"Invalid OAuth access token"}}', 401)
check("invalid token alert", size(notes), 1)
check("alert text", notes[0]["text"], "TOKEN INSTAGRAM INVALIDO")
tick(ig)
http.answer('{"error":{}}', 401)
check("alert at most once a day", size(notes), 1)
check("invalid token waits the whole interval", ig.ticks, 1800)

# --- TikTok: refresh first, rotated refresh token kept, 401 retries once, alert ---
global.notes = []
var tk_set = {"client_key": "ck", "client_secret": "cs", "refresh_token": "rft.seed", "icon": "tiktok", "step": 500, "interval_min": 30, "source": "auto"}
var tk = load("scripts/tiktok.ax", tk_set)
tick(tk)
r = http.answer('{"access_token": "act.1", "expires_in": 86400, "open_id": "x", "refresh_expires_in": 31536000, "refresh_token": "rft.1", "scope": "user.info.basic,user.info.stats", "token_type": "Bearer"}', 200)
check("tiktok refresh method", r["m"], "POST")
check("tiktok refresh url", r["url"], "https://open.tiktokapis.com/v2/oauth/token/")
check("tiktok refresh body", r["body"], "client_key=ck&client_secret=cs&grant_type=refresh_token&refresh_token=rft.seed")
check("tiktok refresh form", r["opts"]["headers"]["Content-Type"], "application/x-www-form-urlencoded")
r = http.answer('{"data":{"user":{"follower_count":1234}},"error":{"code":"ok","message":"","log_id":"x"}}', 200)
check("tiktok info url", r["url"], "https://open.tiktokapis.com/v2/user/info/?fields=follower_count")
check("tiktok bearer", r["opts"]["headers"]["Authorization"], "Bearer act.1")
check("tiktok followers", texts(tk), "1234")
check("rotated refresh token kept", tk.st.get("tok")["rt"], "rft.1")

tick(tk)   # same day: no refresh
r = http.answer('{"data":{"user":{"follower_count":1240}}}', 200)
check("cached access token", r["m"] + " " + r["opts"]["headers"]["Authorization"], "GET Bearer act.1")

T["epoch"] += 23 * 3600 + 1800   # less than 1h left
tick(tk)
r = http.answer('{"access_token":"act.2","expires_in":86400,"refresh_token":"rft.2"}', 200)
check("refresh with the rotated token", r["body"], "client_key=ck&client_secret=cs&grant_type=refresh_token&refresh_token=rft.1")
http.answer('{"data":{"user":{"follower_count":1241}}}', 200)

tick(tk)   # revoked access token: refresh once and ask again
http.answer('{"error":{"code":"access_token_invalid"}}', 401)
r = http.answer('{"access_token":"act.3","expires_in":86400,"refresh_token":"rft.3"}', 200)
check("401 refreshes", r["m"], "POST")
r = http.answer('{"data":{"user":{"follower_count":1250}}}', 200)
check("then asks again", r["opts"]["headers"]["Authorization"], "Bearer act.3")
check("after retry", texts(tk), "1250 +16")

T["epoch"] += 86400   # expired refresh token, reported with a 200 as TikTok may
tick(tk)
http.answer('{"error":"invalid_grant","error_description":"Refresh token is invalid or expired."}', 200)
check("no info request after a failed refresh", size(http.queue), 0)
check("tiktok invalid token alert", size(notes), 1)
check("tiktok alert text", notes[0]["text"], "TOKEN TIKTOK INVALIDO")
check("invalid refresh waits the whole interval", tk.ticks, 1800)
tick(tk)
http.answer('{"error":"invalid_grant"}', 400)
check("tiktok alert at most once a day", size(notes), 1)

tk_set["refresh_token"] = "rft.new"   # a new login starts over from it
tk = load("scripts/tiktok.ax", tk_set)
tick(tk)
r = http.answer('{"access_token":"act.9","expires_in":86400,"refresh_token":"rft.9"}', 200)
check("new seed replaces the stored chain", r["body"], "client_key=ck&client_secret=cs&grant_type=refresh_token&refresh_token=rft.new")
http.answer('{"data":{"user":{"follower_count":1260}}}', 200)

var tk0 = load("scripts/tiktok.ax", {"client_key": "ck", "client_secret": "cs", "refresh_token": "", "icon": "tiktok", "step": 500, "interval_min": 30, "source": "auto"})
tick(tk0)
check("tiktok: no login, no request", size(http.queue), 0)

# --- LinkedIn: from the settings, gray after stale_days without change ---
var li = load("scripts/linkedin.ax", {"followers": 7500, "icon": "32288", "step": 500, "stale_days": 30})
tick(li)
check("no request", size(http.queue), 0)
check("linkedin", texts(li), "7500")
T["epoch"] += 31 * 86400
tick(li)
check("same value: gray after 30 days", li.parts[0][1], 0x666666)
var empty = load("scripts/linkedin.ax", {"followers": 0, "icon": "32288", "step": 500, "stale_days": 30})
tick(empty)
check("0 means not set", empty.should_show(), false)

done()
