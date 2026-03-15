"""Generated protobuf/gRPC code. Add this package's directory to sys.path for imports."""
import sys
from pathlib import Path

# The generated code uses absolute imports like `from optix.marketdata.v1 import types_pb2`.
# We add this directory to sys.path so Python can find the `optix` package.
_gen_dir = str(Path(__file__).parent)
if _gen_dir not in sys.path:
    sys.path.insert(0, _gen_dir)
