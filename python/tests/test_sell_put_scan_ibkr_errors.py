"""compact_ibkr_error pattern coverage (optix #193 finding 1/6a follow-up).

`optix option-quote` now fails fast on a genuine IB per-request error (e.g.
errCode 200 "no security definition" for a bad strike/expiry) instead of
burning the full collection window and reporting the misleading "no usable
price data". The CLI's stderr message changed shape accordingly
("option quote unavailable: <real IB reason>"), so compact_ibkr_error needed
a matching branch — this pins that it extracts the real reason instead of
falling through to the generic truncation, and that the pre-existing
patterns (still-reachable failure modes) are unaffected.
"""
import importlib.util
import os
from pathlib import Path

os.environ["OPTIX_SCAN_RUNTIME_CHECKED"] = "1"
_spec = importlib.util.spec_from_file_location(
    "sell_put_scan", Path(__file__).resolve().parents[2] / "scripts" / "lark_nasdaq100_sell_put_scan.py")
scan = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(scan)


def test_compact_ibkr_error_extracts_real_reason_from_contract_validation_failure():
    msg = ("AAPL 20260717 P 123.45: option quote unavailable: IB error 200: "
           "No security definition has been found for the request")
    got = scan.compact_ibkr_error(msg)
    assert got == ("IBKR option quote invalid: IB error 200: "
                    "No security definition has been found for the request")


def test_compact_ibkr_error_still_matches_generic_no_price_data():
    got = scan.compact_ibkr_error(
        "option quote has no usable price data; warnings: bid_unavailable, no_price_data")
    assert got == "IBKR option quote has no usable price data"


def test_compact_ibkr_error_still_matches_no_price_data_flag():
    assert scan.compact_ibkr_error("no_price_data") == "IBKR option quote no_price_data"


def test_compact_ibkr_error_still_matches_connect_failure():
    assert scan.compact_ibkr_error("IBKR TWS/Gateway not detected on port 4001") == \
        "IBKR unavailable/connect failed"


def test_compact_ibkr_error_falls_back_to_truncation_for_unknown_messages():
    assert scan.compact_ibkr_error("some other unrelated failure") == "some other unrelated failure"
