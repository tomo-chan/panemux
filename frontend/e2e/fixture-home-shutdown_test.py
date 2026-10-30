"""Exercise the actual Playwright shutdown policy against isolated wrapper process groups."""
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

work = Path(sys.argv[1])
servers = json.loads((work / "servers.json").read_text())


def run_case(wrapper, config, shutdown_signal, expect_cleanup, group=True):
    record = work / "signal-record"
    record.unlink(missing_ok=True)
    env = dict(os.environ, PATH=f"{work / 'bin'}:{os.environ['PATH']}",
               TMPDIR=str(work / "tmp"), FIXTURE_RECORD=str(record), FIXTURE_EXIT="hold")
    env.pop("E2E_HOME", None)
    with (work / "signal-output").open("w") as log:
        proc = subprocess.Popen(["sh", str(work / "repo/frontend/e2e" / wrapper), config, "4174"],
                                env=env, stdout=log, stderr=log, start_new_session=True)
        try:
            deadline = time.monotonic() + 10
            while not record.exists() and time.monotonic() < deadline:
                time.sleep(0.02)
            assert record.exists(), "fixture did not start"
            home = Path(record.read_text().splitlines()[0])
            assert home.is_dir(), "fixture HOME missing before shutdown"
            if group:
                os.killpg(proc.pid, shutdown_signal)
            else:
                os.kill(proc.pid, shutdown_signal)
            proc.wait(timeout=5)
            assert home.exists() != expect_cleanup, f"HOME cleanup failed for {config} / {shutdown_signal}"
        finally:
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            proc.wait()


cases = [("4174", "run-panemux-e2e.sh", "agent-board.yml"),
         ("4175", "run-panemux-agent-board-e2e.sh", "agent-board-agmsg.yml"),
         ("4176", "run-panemux-e2e.sh", "command-center.yml")]
for port, wrapper, config in cases:
    server = next(s for s in servers if s["url"].endswith(":" + port))
    policy = server.get("gracefulShutdown", {})
    actual_signal = getattr(signal, policy.get("signal", "SIGKILL"))
    # This reproduces Playwright's SIGKILL leak until the config opts into SIGTERM.
    run_case(wrapper, config, actual_signal, expect_cleanup=True)
    assert policy == {"signal": "SIGTERM", "timeout": 5000}
    run_case(wrapper, config, signal.SIGTERM, expect_cleanup=True, group=False)
# SIGKILL is untrappable: retain a negative control proving why the policy matters.
run_case("run-panemux-e2e.sh", "agent-board.yml", signal.SIGKILL, expect_cleanup=False)
print("fixture-home: configured shutdown, direct SIGTERM and SIGKILL control pass")
