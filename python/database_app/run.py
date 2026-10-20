"""Start/stop the two demo apps and their foreground Agent on this workstation."""

import argparse
import json
import os
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

from .prepare import private_json


def running(pid, config_path):
    result = subprocess.run(["ps", "-p", str(pid), "-o", "command="], capture_output=True, text=True)
    return result.returncode == 0 and str(config_path) in result.stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("start", "stop"))
    parser.add_argument("--root", required=True, type=Path)
    args = parser.parse_args()
    root = args.root.resolve()
    pids_path = root / "processes.json"
    pids = json.loads(pids_path.read_text()) if pids_path.exists() else {}
    units = (("agent", "agent.json"), ("sdk", "sdk.json"), ("agent-app", "agent-app.json"))
    if args.action == "stop":
        for unit, config_name in reversed(units):
            pid = pids.get(unit)
            if pid and running(pid, root / config_name):
                os.kill(pid, signal.SIGTERM)
                print("Stopping", unit, "PID", pid)
        return  # SIGTERM handlers release live DB connections and the Agent socket.
    binary = root / "jms-pam-agent"
    if not binary.exists():
        subprocess.run(["go", "build", "-o", str(binary), "./cmd/jms-pam-agent"],
                       cwd=Path(__file__).resolve().parents[2] / "agent", check=True)
    for unit, config_name in units:
        if pids.get(unit) and running(pids[unit], root / config_name):
            print("Already running:", unit)
            continue
        command = ([str(binary), "run", "--local", "--config", str(root / config_name)] if unit == "agent"
                   else [sys.executable, "-m", "database_app.app", "--config", str(root / config_name)])
        with (root / f"{unit}.log").open("ab") as output:
            child = subprocess.Popen(command, cwd=Path(__file__).resolve().parents[1],
                                     stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        pids[unit] = child.pid
        if pids_path.exists():
            pids_path.unlink()
        private_json(pids_path, pids)
        time.sleep(0.3)
        if child.poll() is not None:
            raise RuntimeError(f"{unit} exited; check its private log under {root}")
        print("Started", unit, "PID", child.pid)
    urls = []
    for filename in ("sdk.json", "agent-app.json"):
        config = json.loads((root / filename).read_text())
        url = f"http://127.0.0.1:{config['http_port']}"
        deadline = time.monotonic() + 30
        while True:
            try:
                with urllib.request.urlopen(url + "/health", timeout=3) as response:
                    if json.load(response)["status"] == "ready":
                        break
            except (OSError, urllib.error.URLError):
                pass
            if time.monotonic() >= deadline:
                raise RuntimeError(f"Demo did not become ready at {url}; inspect its private log")
            time.sleep(0.2)
        urls.append(url)
    print("Ready: SDK", urls[0], "· Agent", urls[1])


if __name__ == "__main__":
    main()
