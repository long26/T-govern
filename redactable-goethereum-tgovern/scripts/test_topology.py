import unittest

import topology


class TopologyTests(unittest.TestCase):
    def test_32_nodes_have_144_edges_and_degree_nine(self):
        topo = topology.build_topology(32)
        self.assertEqual(topo["edge_count"], 144)
        self.assertEqual(topo["max_degree"], 9)
        self.assertEqual(topo["diameter"], 3)
        self.assertEqual(topo["relay_ttl_min"], 4)

    def test_32_tees_plus_miner_fit_the_default_dial_budget(self):
        topo = topology.build_topology(33)
        self.assertEqual(topo["edge_count"], 132)
        self.assertEqual(topo["max_degree"], 8)
        self.assertEqual(topo["diameter"], 3)
        self.assertLess(topo["max_degree"], 50 // 3)

    def test_sparse_topologies_are_connected_symmetric_and_bounded(self):
        for total in (1, 2, 3, 4, 7, 16, 31, 32, 33, 64, 65, 128):
            with self.subTest(total=total):
                topo = topology.build_topology(total)
                self.assertLessEqual(topo["max_degree"], 9)
                self.assertEqual(topo["edge_count"], len(set(topo["edges"])))
                self.assertEqual(2 * topo["edge_count"], sum(map(len, topo["neighbors"].values())))
                for i, peers in topo["neighbors"].items():
                    self.assertNotIn(i, peers)
                    for j in peers:
                        self.assertIn(i, topo["neighbors"][j])
                seen = {1}
                pending = [1]
                while pending:
                    for j in topo["neighbors"][pending.pop()]:
                        if j not in seen:
                            seen.add(j)
                            pending.append(j)
                self.assertEqual(len(seen), total)
                self.assertGreaterEqual(topo["relay_ttl_min"], topo["diameter"])
                self.assertEqual(topo, topology.build_topology(total))

    def test_full_mesh_remains_available_without_sparse_degree_cap(self):
        topo = topology.build_topology(33, mode="full")
        self.assertEqual(topo["edge_count"], 528)
        self.assertEqual(topo["max_degree"], 32)
        self.assertEqual(topo["diameter"], 1)

    def test_radius_and_degree_are_configurable(self):
        topo = topology.build_topology(32, radius=1, max_degree=2)
        self.assertEqual(topo["max_degree"], 2)
        self.assertEqual(topo["diameter"], 16)
        self.assertEqual(topo["relay_ttl_min"], 17)
        self.assertEqual(topology.build_topology(2, radius=1, max_degree=1)["edge_count"], 1)
        self.assertLessEqual(topology.build_topology(32, radius=3, max_degree=8)["max_degree"], 8)

    def test_relay_diameter_excludes_ordinary_shortcuts(self):
        topo = topology.build_topology(33, radius=1, max_degree=2, relay_nodes=range(2, 34))
        self.assertEqual(topo["diameter"], 16)
        self.assertEqual(topo["relay_diameter"], 31)
        self.assertEqual(topo["relay_ttl_min"], 32)
        with self.assertRaisesRegex(ValueError, "disconnected"):
            topology.build_topology(4, radius=1, max_degree=2, relay_nodes=[1, 3])
        for nodes in ([], [0], [5]):
            with self.assertRaises(ValueError):
                topology.build_topology(4, relay_nodes=nodes)

    def test_invalid_or_disconnected_requests_fail_early(self):
        for kwargs in ({"total": 0}, {"total": -1}, {"total": 32, "mode": "invalid"},
                       {"total": 32, "radius": 0}, {"total": 32, "max_degree": 0},
                       {"total": 32, "radius": 2, "max_degree": 3},
                       {"total": 3, "radius": 1, "max_degree": 1}):
            with self.subTest(kwargs=kwargs), self.assertRaises(ValueError):
                topology.build_topology(**kwargs)


if __name__ == "__main__":
    unittest.main()
