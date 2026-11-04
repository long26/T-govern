#!/usr/bin/env python3

import collections
import math
import unittest

from scripts import sgx_l5_sim_e2e as orchestration


def adjacency(nodes, edges):
    graph = {node: set() for node in nodes}
    for source, target in edges:
        graph[source].add(target)
        graph[target].add(source)
    return graph


class TopologyTests(unittest.TestCase):
    def test_tee_identity_readiness_requires_complete_nonzero_hex_fields(self):
        ready = {
            "HardwareID": "0x01",
            "SigPubKey": "0x02",
            "VRFPubKey": "0x03",
            "EnclavePK": "0x04",
        }
        self.assertTrue(orchestration.tee_identity_is_ready(ready))
        self.assertFalse(orchestration.tee_identity_is_ready({**ready, "EnclavePK": "0x00"}))
        self.assertFalse(orchestration.tee_identity_is_ready({**ready, "SigPubKey": "not-hex"}))
        self.assertFalse(orchestration.tee_identity_is_ready({"HardwareID": "0x01"}))

    def test_64_tee_hosts_are_dense_but_not_fully_connected(self):
        nodes = list(range(1, 65))
        edges = orchestration.merge_topology_edges(
            orchestration.ring_neighbor_edges(nodes, 8),
            orchestration.power_ring_edges(nodes, math.ceil(math.log2(len(nodes)))),
        )
        graph = adjacency(nodes, edges)

        self.assertTrue(all(len(graph[node]) >= 16 for node in nodes))
        self.assertTrue(all(len(graph[node]) < len(nodes) - 1 for node in nodes))
        self.assertLess(len(edges), len(nodes) * (len(nodes) - 1) // 2)

    def test_2048_light_node_overlay_is_sparse_and_connected(self):
        nodes = list(range(1, 2050))  # one ON gateway plus 2048 light nodes
        levels = math.ceil(math.log2(len(nodes)))
        edges = orchestration.power_ring_edges(nodes, levels)
        graph = adjacency(nodes, edges)

        visited = {nodes[0]}
        queue = collections.deque(visited)
        while queue:
            current = queue.popleft()
            for target in graph[current]:
                if target not in visited:
                    visited.add(target)
                    queue.append(target)

        self.assertEqual(visited, set(nodes))
        self.assertLess(len(edges), len(nodes) * levels)
        self.assertLess(max(map(len, graph.values())), 2 * levels + 1)
        self.assertLess(len(edges), len(nodes) * (len(nodes) - 1) // 100)

    def test_32_tee_and_1024_light_acceptance_topology_is_connected(self):
        gateway = [1]
        tee_nodes = list(range(2, 34))
        light_nodes = list(range(34, 1058))
        ordinary_nodes = gateway + light_nodes
        edges = orchestration.merge_topology_edges(
            orchestration.ring_neighbor_edges(tee_nodes, 8),
            orchestration.power_ring_edges(tee_nodes, math.ceil(math.log2(len(tee_nodes)))),
            orchestration.gateway_bridge_edges(gateway, tee_nodes, 8),
            orchestration.power_ring_edges(
                ordinary_nodes, math.ceil(math.log2(len(ordinary_nodes)))
            ),
        )
        nodes = gateway + tee_nodes + light_nodes
        graph = adjacency(nodes, edges)
        visited = {gateway[0]}
        queue = collections.deque(visited)
        while queue:
            current = queue.popleft()
            for target in graph[current]:
                if target not in visited:
                    visited.add(target)
                    queue.append(target)

        self.assertEqual(visited, set(nodes))
        self.assertLess(max(map(len, graph.values())), 32)
        self.assertLess(len(edges), len(nodes) * 24)

    def test_hop_bound_covers_actual_power_ring_diameter(self):
        nodes = list(range(1, 66))
        levels = math.ceil(math.log2(len(nodes)))
        graph = adjacency(nodes, orchestration.power_ring_edges(nodes, levels))
        actual_diameter = 0
        for source in nodes:
            distances = {source: 0}
            queue = collections.deque([source])
            while queue:
                current = queue.popleft()
                for target in graph[current]:
                    if target not in distances:
                        distances[target] = distances[current] + 1
                        queue.append(target)
            actual_diameter = max(actual_diameter, max(distances.values()))

        self.assertLessEqual(
            actual_diameter,
            orchestration.power_ring_hop_bound(len(nodes), levels),
        )


if __name__ == "__main__":
    unittest.main()
