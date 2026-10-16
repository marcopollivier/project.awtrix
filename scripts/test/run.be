# Entry point: install the NG stand-ins first, then compile the tests (Berry
# resolves names at compile time, so the stubs must exist by then).
#   berry scripts/test/run.be   (from the repository root)
compile("scripts/test/ng.be", "file")()
compile("scripts/test/scripts_test.be", "file")()
