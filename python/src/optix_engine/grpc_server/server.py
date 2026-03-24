"""Python gRPC analysis server entry point."""

import argparse
import logging
import os
import signal
import sys
from concurrent import futures
from pathlib import Path

import grpc

import optix_engine.gen  # fixes sys.path for proto imports
from optix.analysis.v1 import analysis_pb2_grpc

from optix_engine.grpc_server.analysis_servicer import AnalysisServicer

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
)
log = logging.getLogger(__name__)


def serve(addr: str = "localhost:50052", workers: int = 4, ready_file: str | None = None) -> None:
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=workers))
    analysis_pb2_grpc.add_AnalysisServiceServicer_to_server(AnalysisServicer(), server)

    server.add_insecure_port(addr)
    server.start()
    log.info("Analysis gRPC server started on %s (workers=%d)", addr, workers)

    # Signal readiness to the parent process via a sentinel file.
    # The caller sets OPTIX_READY_FILE or --ready-file; the shell script
    # watches for this file instead of polling with nc -z.
    if ready_file:
        Path(ready_file).touch()
        log.info("Ready signal written to %s", ready_file)

    def _shutdown(sig, frame):
        log.info("Shutting down...")
        if ready_file:
            try:
                Path(ready_file).unlink(missing_ok=True)
            except OSError:
                pass
        server.stop(grace=5)
        sys.exit(0)

    signal.signal(signal.SIGINT, _shutdown)
    signal.signal(signal.SIGTERM, _shutdown)
    server.wait_for_termination()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Optix Python analysis gRPC server")
    parser.add_argument("--addr", default="localhost:50052", help="Listen address")
    parser.add_argument("--workers", type=int, default=4, help="Thread pool workers")
    parser.add_argument("--ready-file", default=os.environ.get("OPTIX_READY_FILE"),
                        help="Write a sentinel file when server is ready (for startup orchestration)")
    args = parser.parse_args()
    serve(args.addr, args.workers, args.ready_file)
