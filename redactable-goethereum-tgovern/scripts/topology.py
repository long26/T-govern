"""Deterministic, bounded-degree P2P topologies for multi-host experiments.

Edges are undirected and node indices are one-based, matching geth workspaces.
Sparse mode starts with a ring and adds power-of-two shortcuts within the cap.
"""

from __future__ import annotations

from collections import deque


def graph_diameter(neighbors: dict[int, list[int] | set[int]], nodes=None) -> int:
    """Measure an induced subgraph; ordinary nodes are not TEE relay hops."""
    allowed = set(neighbors if nodes is None else nodes)
    if not allowed or not allowed.issubset(neighbors):
        raise ValueError("relay nodes must be a nonempty subset of topology nodes")
    diameter = 0
    for start in sorted(allowed):
        distances = {start: 0}
        queue = deque([start])
        while queue:
            node = queue.popleft()
            for peer in neighbors[node]:
                if peer in allowed and peer not in distances:
                    distances[peer] = distances[node] + 1
                    queue.append(peer)
        if len(distances) != len(allowed):
            raise ValueError("topology is disconnected")
        diameter = max(diameter, max(distances.values()))
    return diameter


def build_topology(total: int, mode: str = "sparse", radius: int = 2,
                   max_degree: int = 9, relay_nodes=None) -> dict:
    if total < 1:
        raise ValueError("topology needs at least one node")
    if mode not in ("sparse", "full"):
        raise ValueError("topology mode must be sparse or full")
    if radius < 1 or max_degree < 1:
        raise ValueError("topology radius and max degree must be positive")
    neighbors = {i: set() for i in range(1, total + 1)}

    def add_offset(offset: int) -> bool:
        additions = {i: set() for i in neighbors}
        for i in neighbors:
            j = (i - 1 + offset) % total + 1
            if i != j:
                additions[i].add(j)
                additions[j].add(i)
        if any(len(neighbors[i] | additions[i]) > max_degree for i in neighbors):
            return False
        for i in neighbors:
            neighbors[i].update(additions[i])
        return True

    if mode == "full":
        for i in neighbors:
            neighbors[i].update(j for j in neighbors if j != i)
    else:
        for offset in range(1, min(radius, total // 2) + 1):
            if not add_offset(offset):
                raise ValueError("topology max degree is too small for the requested ring radius")
        offset = 1
        while offset <= total // 2:
            if offset > radius:
                add_offset(offset)
            offset *= 2

    diameter = graph_diameter(neighbors)
    relay_diameter = graph_diameter(neighbors, relay_nodes) if relay_nodes is not None else diameter
    edges = [(i, j) for i in neighbors for j in sorted(neighbors[i]) if i < j]
    return {"mode": mode, "total": total, "radius": radius, "degree_limit": max_degree,
            "edges": edges, "neighbors": {i: sorted(peers) for i, peers in neighbors.items()},
            "edge_count": len(edges), "max_degree": max(map(len, neighbors.values())),
            "diameter": diameter, "relay_diameter": relay_diameter,
            "relay_ttl_min": max(diameter, relay_diameter) + 1}
