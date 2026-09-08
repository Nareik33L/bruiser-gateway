import json
import os
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from bruiser import FenceCache, Protect, StaleFence, public_from_jwks, verify  # noqa: E402


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


if __name__ == "__main__":
    unittest.main()
