import json
import os
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from http.server import BaseHTTPRequestHandler, HTTPServer
from threading import Thread

from bruiser import Client, FenceCache, Protect, StaleFence, public_from_jwks, verify  # noqa: E402


def mint():
    r = subprocess.run(
        ["go", "run", "./sdk/mint"],
        cwd=Path(__file__).resolve().parents[3],
        capture_output=True,
        text=True,
        check=False,
    )
    if r.returncode != 0:
        raise unittest.SkipTest(f"go mint unavailable: {r.stderr}")
    return json.loads(r.stdout)


class TestProtect(unittest.TestCase):
    def test_verify_and_stale_fence(self):
        try:
            minted = mint()
        except unittest.SkipTest:
            if os.environ.get("CI"):
                raise
            self.skipTest("go mint unavailable")
        pub = public_from_jwks(minted["jwks"])
        claims = verify(minted["ok"], pub)
        self.assertEqual(claims["exe"], "exe_1")
        fences = FenceCache()
        fences.accept(claims["dom"], claims["fnc"])
        stale = verify(minted["stale"], pub)
        with self.assertRaises(StaleFence):
            fences.accept(stale["dom"], stale["fnc"])
        p = Protect(public=pub)
        got = p({"X-Bruiser-Execution": minted["ok"]})
        self.assertEqual(got["exe"], "exe_1")
        with self.assertRaises(StaleFence):
            p({"X-Bruiser-Execution": minted["stale"]})

    def test_agent_client(self):
        class H(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                return

            def _write(self, code, body):
                raw = json.dumps(body).encode()
                self.send_response(code)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(raw)

            def do_POST(self):
                if self.path == "/v1/sessions":
                    self._write(201, {"session_id": "ses_1", "session_token": "sess", "customer_id": "alice"})
                    return
                if self.path == "/v1/executions/acquire":
                    self._write(201, {"execution_id": "exe_1", "status": "ACTIVE"})
                    return
                if self.path == "/v1/executions/exe_1/renew":
                    self._write(200, {"execution_id": "exe_1", "status": "ACTIVE"})
                    return
                if self.path == "/v1/executions/exe_1/release":
                    self._write(200, {"execution_id": "exe_1", "state": "RELEASED"})
                    return
                self._write(404, {})

        srv = HTTPServer(("127.0.0.1", 0), H)
        Thread(target=srv.serve_forever, daemon=True).start()
        try:
            c = Client(f"http://127.0.0.1:{srv.server_address[1]}")
            sess = c.create_session("assert", "agent", "a1")
            self.assertEqual(sess["session_token"], "sess")
            exe = c.acquire(sess["session_token"], "event:x")
            self.assertEqual(exe["execution_id"], "exe_1")
            c.renew(sess["session_token"], exe["execution_id"])
            c.release(sess["session_token"], exe["execution_id"])
        finally:
            srv.shutdown()


if __name__ == "__main__":
    unittest.main()
