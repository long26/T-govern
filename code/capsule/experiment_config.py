"""Source and offline-cache ownership for each supported experiment."""
from dataclasses import dataclass


@dataclass(frozen=True)
class Experiment:
    modules: tuple[str, ...]
    cache_name: str


SGX = Experiment(('committee', 'redactable-goethereum-tgovern'), 'go-mod-cache.tar.gz')
EXPERIMENTS = {
    'sgx': SGX,
    'shamir': SGX,
    'nitch': Experiment(('redactable-goethereum-wolverine',), 'nitch-go-mod-cache.tar.gz'),
}
