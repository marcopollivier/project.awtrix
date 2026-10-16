# Static checks on scripts/*.ax, with the NG stand-ins of ng.be installed so
# names the firmware provides (store, http, re...) resolve at compile time.
#   berry scripts/test/lint.be   (from the repository root)
#
# - every script compiles: syntax errors and undeclared names fail here;
# - "# @config" lines sit in the header, before any code (the firmware only
#   reads the header);
# - only counter.ax is a @module.
compile("scripts/test/ng.be", "file")()
import global
import os
import string

var errors = 0
def fail(path, msg)
  print("ERRO " + path + ": " + msg)
  errors += 1
end

def read(path)
  var f = open(path)
  var src = f.read()
  f.close()
  return src
end

# header checks: line by line, the header ends at the first line that is
# neither blank nor a comment.
def check_header(path, src)
  var code = 0          # line number where code starts, 0 while in the header
  var n = 0
  for line : string.split(src, "\n")
    n += 1
    var t = string.tr(line, "\r", "")
    if code == 0 && size(t) > 0 && t[0] != "#"
      code = n
    end
    if string.find(t, "# @config") == 0 && code > 0
      fail(path, "linha " + str(n) + ": @config depois do código (linha " + str(code) + "); o firmware só lê o cabeçalho")
    end
    if string.find(t, "# @module") == 0 && path != "scripts/counter.ax"
      fail(path, "só counter.ax pode ser @module")
    end
  end
end

# compile one script; "import counter" is resolved to global.counter, as
# load() in ng.be does.
def check_compile(path, src)
  try
    return compile(string.replace(src, "import counter\n", "\n"))
  except .. as e, m
    fail(path, str(e) + ": " + str(m))
  end
  return nil
end

var names = []
for n : os.listdir("scripts")
  if size(n) > 3 && n[-3 ..] == ".ax" names.push(n) end
end
if names.find("counter.ax") == nil
  fail("scripts/", "falta counter.ax")
end

# counter.ax first: the apps reference the module it returns.
var src = read("scripts/counter.ax")
check_header("scripts/counter.ax", src)
global.store = global.Store({})
var fn = check_compile("scripts/counter.ax", src)
if fn != nil
  try
    global.counter = fn()
  except .. as e, m
    fail("scripts/counter.ax", "erro ao carregar o módulo: " + str(e) + ": " + str(m))
  end
end

for n : names
  if n == "counter.ax" continue end
  var path = "scripts/" + n
  var s = read(path)
  check_header(path, s)
  if string.find(s, "import counter") < 0
    fail(path, "app sem import counter")
  end
  check_compile(path, s)
end

if errors > 0
  print(str(errors) + " erro(s) em " + str(size(names)) + " script(s)")
  os.exit(1)
end
print("ok: " + str(size(names)) + " script(s)")
