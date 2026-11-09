import unittest

from verdict import MIB, check


class WorkingSetTests(unittest.TestCase):
    def row(self, hop, criterion):
        return {
            "result": {"status": 200, "completed": True},
            "compare": {"mock_saw": True, "sha_equal": True},
            "mem": {hop: {"peak_rung": 512 * MIB,
                          "stat_max": {"keeper_criterion": criterion}}},
        }

    def test_keeper_and_aisix_use_working_set_with_unchanged_margin(self):
        for hop in ("keeper", "aisix"):
            with self.subTest(hop=hop):
                self.assertEqual(check(self.row(hop, 480 * MIB), {hop: 512 * MIB}, {}), [])
                failures = check(self.row(hop, 480 * MIB + 1), {hop: 512 * MIB}, {})
                self.assertEqual(len(failures), 1)
                self.assertIn("keeper_criterion", failures[0])

    def test_other_hops_still_use_raw_peak(self):
        for hop in ("bridge", "sub2api", "cpa", "firewall", "apisix-models", "squid"):
            with self.subTest(hop=hop):
                failures = check(self.row(hop, 26 * MIB), {hop: 512 * MIB}, {})
                self.assertEqual(len(failures), 1)
                self.assertIn("memory.peak", failures[0])

    def test_missing_working_set_falls_back_to_raw_peak(self):
        for hop in ("keeper", "aisix"):
            with self.subTest(hop=hop):
                failures = check(self.row(hop, None), {hop: 512 * MIB}, {})
                self.assertEqual(len(failures), 1)
                self.assertIn("memory.peak", failures[0])


if __name__ == "__main__":
    unittest.main()
