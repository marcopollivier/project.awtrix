# Stand-ins for the AWTRIX NG script runtime, so scripts/*.ax run on a desktop
# Berry interpreter (https://github.com/berry-lang/berry). Only what the
# scripts use is here. The real firmware is the reference; these stubs exist to
# catch syntax and logic errors before installing on the clock.
import global
import string

# --- re: a tiny backtracking regex (literals, \d, [^x], groups, * and +) ---
class TinyRe
  static def parse(p)
    var toks = []
    var i = 0
    while i < size(p)
      var c = p[i]
      var t
      if c == "\\"
        i += 1
        t = p[i] == "d" ? {"k": "digit"} : {"k": "lit", "c": p[i]}
      elif c == "["
        var j = i + 2   # only [^...] is supported
        var set = ""
        while p[j] != "]"
          set += p[j]
          j += 1
        end
        t = {"k": "not", "set": set}
        i = j
      elif c == "("
        t = {"k": "open"}
      elif c == ")"
        t = {"k": "close"}
      else
        t = {"k": "lit", "c": c}
      end
      if i + 1 < size(p) && (p[i + 1] == "*" || p[i + 1] == "+")
        t["q"] = p[i + 1]
        i += 1
      end
      toks.push(t)
      i += 1
    end
    return toks
  end

  static def one(t, ch)
    if t["k"] == "digit" return ch >= "0" && ch <= "9" end
    if t["k"] == "not" return string.find(t["set"], ch) < 0 end
    return t["c"] == ch
  end

  # m matches toks[ti..] at s[si..]; groups collects [start, end] pairs.
  static def m(toks, ti, s, si, groups)
    if ti == size(toks) return si end
    var t = toks[ti]
    if t["k"] == "open"
      groups.push([si, -1])
      var r = TinyRe.m(toks, ti + 1, s, si, groups)
      if r < 0 groups.pop() end
      return r
    end
    if t["k"] == "close"
      for g : groups
        if g[1] < 0 g[1] = si end   # close the innermost open group
      end
      return TinyRe.m(toks, ti + 1, s, si, groups)
    end
    var q = t.find("q")
    if q == nil
      if si < size(s) && TinyRe.one(t, s[si]) return TinyRe.m(toks, ti + 1, s, si + 1, groups) end
      return -1
    end
    var n = 0
    while si + n < size(s) && TinyRe.one(t, s[si + n]) n += 1 end
    var least = q == "+" ? 1 : 0
    while n >= least
      var r = TinyRe.m(toks, ti + 1, s, si + n, groups)
      if r >= 0 return r end
      n -= 1
    end
    return -1
  end

  static def at(p, s, from)
    var toks = TinyRe.parse(p)
    for start : from .. size(s)
      var groups = []
      var e = TinyRe.m(toks, 0, s, start, groups)
      if e >= 0
        var out = [s[start .. e - 1]]
        for g : groups out.push(s[g[0] .. g[1] - 1]) end
        return [out, e]
      end
    end
    return nil
  end
end

var re = module("re")
re.search = def (p, s)
  var r = TinyRe.at(p, s, 0)
  return r == nil ? nil : r[0]
end
re.matchall = def (p, s)
  var out = []
  var from = 0
  while true
    var r = TinyRe.at(p, s, from)
    if r == nil break end
    out.push(r[0][0])
    from = r[1] > from ? r[1] : from + 1
  end
  return out
end
global.re = re

# --- store: one per script ---
class Store
  var data
  def init(defaults) self.data = defaults end
  def get(k, d)
    var v = self.data.find(k)
    return v == nil ? d : v
  end
  def set(k, v) self.data[k] = v end
end

# --- http: requests wait in a queue; the test answers them ---
class FakeHTTP
  var queue
  def init() self.queue = [] end
  def get(url, cb, opts) self.queue.push({"m": "GET", "url": url, "cb": cb, "opts": opts}) end
  def post(url, body, cb, opts) self.queue.push({"m": "POST", "url": url, "body": body, "cb": cb, "opts": opts}) end

  # answer the oldest request, applying find/keep like the firmware.
  def answer(body, status)
    var r = self.queue[0]
    self.queue.remove(0)
    var o = r["opts"]
    if body != nil && o != nil && o.find("find") != nil
      var i = string.find(body, o["find"])
      body = i < 0 ? nil : body[i .. i + o.find("keep", 256) - 1]
    end
    r["cb"](body, status)
    return r
  end
end
global.http = FakeHTTP()

# --- clock, display, notifications (tests change these through global.*) ---
global.T = {"epoch": 1792170000, "mono": 1000000, "y": 2026, "mo": 10, "d": 16}
global.epoch_ms = def () return global.T["epoch"] * 1000 end
global.now_ms = def () return global.T["mono"] end
global.year = def () return global.T["y"] end
global.month = def () return global.T["mo"] end
global.day = def () return global.T["d"] end
global.notes = []
global.notify = def (n) global.notes.push(n) return true end
global.logs = []
global.log = def (s) global.logs.push(s) end
global.frame = []
global.width = def () return 32 end
global.clear = def () global.frame.clear() end
global.icon = def (n, x, y) global.frame.push(["icon", n]) return true end
global.scroll_text = def (x, y, w, parts, c) global.frame.push(["text", parts]) return 0 end
global.progress = def (p, c, bg, x0) global.frame.push(["progress", p]) end

global.counter = nil   # set by the tests once the module is loaded

# load runs one .ax file with its own store and returns what it returns.
# "import counter" is resolved to the module loaded earlier.
def load(path, settings)
  var f = open(path)
  var src = f.read()
  f.close()
  src = string.replace(src, "import counter\n", "\n")
  global.store = Store(settings)
  var fn = compile(src)
  return fn()
end
global.load = load

var fails = 0
global.check = def (name, got, want)
  if str(got) != str(want)
    print("FALHOU " + name + ": got " + str(got) + " want " + str(want))
    fails += 1
  end
end
global.done = def ()
  if fails > 0
    print(str(fails) + " falha(s)")
    import os
    os.exit(1)
  end
  print("ok")
end
