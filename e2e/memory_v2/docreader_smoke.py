#!/usr/bin/env python3
"""Exercise real unary/streaming gRPC parsing in the isolated release Compose project."""
import subprocess

script = '''
import grpc
from docreader.proto import docreader_pb2 as pb, docreader_pb2_grpc as rpc

text = "# Memory V2 release gate\\n\\nKiểm chứng tài liệu tiếng Việt với PostgreSQL isolated."
with grpc.insecure_channel("localhost:50051") as channel:
    stub = rpc.DocReaderStub(channel)
    engines = stub.ListEngines(pb.ListEnginesRequest(), timeout=20)
    assert any(e.available and "md" in e.file_types for e in engines.engines)
    request = pb.ReadRequest(
        file_content=text.encode(), file_name="release-smoke.md", file_type="md",
        request_id="memory-v2-release-gate",
    )
    unary = stub.Read(request, timeout=60)
    assert not unary.error, unary.error
    assert "Kiểm chứng tài liệu tiếng Việt" in unary.markdown_content
    frames = list(stub.ReadStream(request, timeout=60))
    assert frames and frames[0].WhichOneof("payload") == "meta"
    assert not frames[0].meta.error, frames[0].meta.error
    assert frames[0].meta.markdown_content == unary.markdown_content
    print("PASS: docreader ListEngines, Read and ReadStream preserve Vietnamese UTF-8 Markdown")
'''
subprocess.run([
    'docker', 'exec', '-i', 'memv2-release-gate-docreader-1',
    '/app/.venv/bin/python', '-',
], input=script, text=True, check=True)
