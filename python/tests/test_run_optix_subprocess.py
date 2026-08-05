"""Graceful terminate-then-kill subprocess helper (IBKR session hygiene fix)."""
import importlib.util
import os
import subprocess
import time
from pathlib import Path

os.environ["OPTIX_SCAN_RUNTIME_CHECKED"] = "1"
_spec = importlib.util.spec_from_file_location(
    "sell_put_scan", Path(__file__).resolve().parents[2] / "scripts" / "lark_nasdaq100_sell_put_scan.py")
scan = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(scan)


def test_completes_within_timeout():
    cp = scan.run_optix_subprocess(["/bin/sh", "-c", "echo ok"], timeout=5)
    assert cp.returncode == 0 and cp.stdout.strip() == "ok"


def test_stdin_text_is_piped_to_child():
    cp = scan.run_optix_subprocess(["/bin/sh", "-c", "cat"], timeout=5, stdin_text="hello")
    assert cp.returncode == 0 and cp.stdout == "hello"


def test_timeout_sends_sigterm_first():
    # 子进程 trap TERM 后写标记文件再退出——证明先收到的是 TERM 而非 KILL
    marker = Path(os.environ.get("TMPDIR", "/tmp")) / f"optix_term_test_{os.getpid()}"
    marker.unlink(missing_ok=True)
    script = f'trap "echo got-term > {marker}; exit 143" TERM; sleep 30'
    start = time.time()
    try:
        scan.run_optix_subprocess(["/bin/sh", "-c", script], timeout=1)
        raised = False
    except subprocess.TimeoutExpired:
        raised = True
    elapsed = time.time() - start
    assert raised
    assert elapsed < 10  # 1s 超时 + ≤5s 宽限,远小于 sleep 30
    deadline = time.time() + 3
    while time.time() < deadline and not marker.exists():
        time.sleep(0.05)
    assert marker.exists(), "child never received SIGTERM (got SIGKILL directly?)"
    marker.unlink(missing_ok=True)
