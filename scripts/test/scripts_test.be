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

# --- formatting, like render.Count in Go ---
counter_with("")
var c = counter.Counter(global.Store({}), counter.cfg, "k", "K")
for tc : [[793, "793"], [4521, "4521"], [12345, "12.3K"], [12978, "13K"], [99999, "100K"],
          [128000, "128K"], [1234567, "1.2M"], [12000000, "12M"], [-3, "-3"]]
  check("count " + str(tc[0]), c.count(tc[0]), tc[1])
end
check("today", c.today(), 20261016)

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
