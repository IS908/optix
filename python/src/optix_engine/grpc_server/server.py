"""Python gRPC analysis server entry point."""

import argparse
import logging
import signal
import sys
from concurrent import futures

import grpc

import optix_engine.gen  # fixes sys.path for proto imports
from optix.analysis.v1 import analysis_pb2_grpc

from optix_engine.grpc_server.analysis_servicer import AnalysisServicer

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
)
log = logging.getLogger(__name__)


def serve(addr: str = "localhost:50052", workers: int = 4) -> None:
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=workers))
    analysis_pb2_grpc.add_AnalysisServiceServicer_to_server(AnalysisServicer(), server)

    server.add_insecure_port(addr)
    server.start()
    log.info("Analysis gRPC server started on %s (workers=%d)", addr, workers)

    def _shutdown(sig, frame):
        log.info("Shutting down...")
        server.stop(grace=5)
        sys.exit(0)

    signal.signal(signal.SIGINT, _shutdown)
    signal.signal(signal.SIGTERM, _shutdown)
    server.wait_for_termination()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Optix Python analysis gRPC server")
    parser.add_argument("--addr", default="localhost:50052", help="Listen address")
    parser.add_argument("--workers", type=int, default=4, help="Thread pool workers")
    args = parser.parse_args()
    serve(args.addr, args.workers)
