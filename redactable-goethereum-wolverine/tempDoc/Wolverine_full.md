# Li 等 - 2023 - Wolverine A Scalable and Transaction-Consistent Redactable Permissionless Blockchain.pdf

来源: Li 等 - 2023 - Wolverine A Scalable and Transaction-Consistent Redactable Permissionless Blockchain.pdf

## 第 1 页

IEEE TRANSACTIONS ON INFORMATION FORENSICS AND SECURITY, VOL. 18, 2023
1653
Wolverine: A Scalable and Transaction-Consistent
Redactable Permissionless Blockchain
Jianhao Li , Hui Ma , Jiabei Wang , Zishuai Song , Wenhan Xu , and Rui Zhang
Abstract— The immutability of blockchains is critical for
cryptocurrencies, but an imperative need arises for the redaction
of on-chain data due to privacy-protecting laws like GPDR.
Recently, Ateniese et al. (EuroS&P 2017) proposed an elegant
solution to this problem based on chameleon hash functions,
followed by many subsequent works. While these works offered
a solution to the permissioned blockchain, the approaches were
not efficient enough for the permissionless setting, in terms of
either security (which may cause inconsistent historical trans-
actions) or performance (only up to a few hundred nodes).
In this paper, we investigate this problem and present Wolverine,
a redactable permissionless blockchain. First, we present a formal
redactable blockchain model, carefully considering transaction
consistency. Next, towards a practical scheme, we introduce
the novel concept of non-interactive chameleon hash (NITCH).
NITCHs dynamically distribute a trapdoor key among a group
and each party in the group can compute its partial share without
communicating with others. Anyone who possesses enough shares
can then find a valid hash collision. To prevent the static
group from being compromised after a sufficiently long time,
we provide a generic transform from NITCHs to decentralized
random beacons (DRBs) and design a committee evolution
protocol based on DRBs that refresh the group after every fixed
interval of time. Based on NITCH and the committee evolution
protocol, we construct Wolverine which offers important features
such as scalability, transaction consistency, and public account-
ability. Finally, we demonstrate the practicality of Wolverine
by giving a proof-of-concept implementation based on Bitcoin
in Golang.
Index Terms— Redactable blockchain, non-interactive thresh-
old chameleon hash, transaction consistency, scalability.
I. INTRODUCTION
I
N THE past few years, blockchain technologies have
received much attention due to the success of Bitcoin [1].
Manuscript received 14 September 2022; revised 15 January 2023;
accepted 25 January 2023. Date of publication 14 February 2023; date
of current version 2 March 2023. This work was supported in part by
the National Natural Science Foundation of China under Grant 62172411,
Grant 62172404, Grant 61972094, and Grant 62202458. The associate editor
coordinating the review of this manuscript and approving it for publication was
Prof. Edgar Weippl. (Corresponding author: Rui Zhang.)
Jianhao Li, Hui Ma, Zishuai Song, Wenhan Xu, and Rui Zhang are with
the State Key Laboratory of Information Security, Institute of Information
Engineering, Chinese Academy of Sciences, Beijing 100093, China, and also
with the School of Cyber Security, University of Chinese Academy of Sci-
ences, Beijing 100049, China (e-mail: lijianhao@iie.ac.cn; mahui@iie.ac.cn;
songzishuai@iie.ac.cn; xuwenhan@iie.ac.cn; r-zhang@iie.ac.cn).
Jiabei Wang is with the School of Cyber Science and Engineering, Nanjing
University of Science and Technology, Nanjing 210094, China (e-mail:
wangjiabei@njust.edu.cn).
This
article
has
supplementary
downloadable
material
available
at
https://doi.org/10.1109/TIFS.2023.3245406, provided by the authors.
Digital Object Identifier 10.1109/TIFS.2023.3245406
A blockchain can be viewed as a hash-chain, where each new
block contains the hash of the previous block and a set of
valid transactions that are decided by a consensus mechanism
(e.g., Proof-of-Work in Bitcoin). There are two fundamentally
different blockchain models: permissioned and permissionless
blockchain. Permissioned blockchains (aka. private blockchain
or consortium blockchain) like Ripple, Hyperlegder, are closed
networks where nodes require special permission from the
network administrators to join the network. In contrast, per-
missionless blockchains (aka. public blockchain) like Bitcoin,
Ethereum, are open networks available to everyone to join
or leave the networks, and heavy use of cryptocurrencies as
incentives to participate.
Immutability
is
essential
to
blockchains,
which
was
inevitably accompanied by abuses. It is well-known that
malicious users can append transactions containing illicit
data into a blockchain [2]. Further research on Bitcoin by
Matzutt et al. [3] revealed that there are more than 1600 files
containing inappropriate videos, personal private information,
and materials infringing on intellectual rights. Therefore,
International Criminal Police Organization (INTERPOL) [4]
viewed blockchain as “a forensic challenge to the current law
enforcement system”. Moreover, the general data protection
regulation (GDPR) [5] of the European Union expressively
imposes the “Right to be Forgotten” as a key data subject
right, which the current blockchain technology cannot comply
with obviously.
However, redacting data on a blockchain may result in
serious inconsistencies. As shown in Fig. 1, there are two
links in Bitcoin: the hash links and the signature links. The
hash link connects two blocks by embedding the hash of
the previous block into the next block header. The signature
link is the witness which satisfies the challenge script of the
previous transaction. If a transaction (e.g., tx1
2) is modified,
it will not only break the hash link between two adjacent
blocks (B2 ←B3), but also the signature links between
transactions (tx0
0 L99 tx1
2, tx1
0 L99 tx1
2, tx1
2 L99 tx1
3 and
tx1
2 L99 tx2
3), thus causing some transactions (tx0
0, tx1
0) to be
double-spent and some transactions (tx1
3, tx2
3) to be invalid.
In addition, the feature of redaction may be exploited by
adversaries, such as modifying the balance of some output
scripts.
Therefore, a redaction must be performed under strict con-
straints of correctness: including integrity of the hash links,
transaction consistency, i.e., integrity of the signature links,
and immutability of state (the balance of each output script).
1556-6021 © 2023 IEEE. Personal use is permitted, but republication/redistribution requires IEEE permission.
See https://www.ieee.org/publications/rights/index.html for more information.
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


## 第 2 页

1654
IEEE TRANSACTIONS ON INFORMATION FORENSICS AND SECURITY, VOL. 18, 2023
Fig. 1.
Hash links and signature links in Bitcoin.
We also insist that a redaction be instant to prevent the
dissemination of harmful content.
A. Related Work
Ateniese et al. [6] first introduced the notion of redactable
blockchain and proposed a solution based on chameleon
hash functions [7]. They replace the regular hash function
of chaining the different blocks together with a chameleon
hash function. The trapdoor of the chameleon hash function
is secretly shared among all parties of the network, enables
block-level redactability since a collision of the hash value
can be easily found by the trapdoor holders with a secure
multiparty computation (MPC) [8], [9]. To remark, this solu-
tion does not work well in a permissionless blockchain, due
to the following two facts. First, the scheme may be easily
defeated by Sybil attacks [10] in the absence of public-key
infrastructure (PKI), where an adversary can repeatedly join
malicious parties with fresh identities to gain significant
influence on the protocol outcome. Second, an MPC protocol
such as [8] and [9] may require O(poly(n) · |C|) com-
munication complexity, where n is the number of parties,
and C is an arithmetic circuit computing the collision of a
chameleon hash. Hence this approach is bandwidth-limited
and has a poor performance when n
goes to a few
hundred.
In order to support fine-grained and controlled redaction
of blockchain, Derler et al. [11] proposed a variant of
chameleon hash named policy-based chameleon hash (PCH)
that allows only authorized users to find collisions via the
use of attribute-based encryption (ABE) [12]. By replac-
ing the regular hash function used to compute the Merkle
tree’s leaf node with PCH, transaction-level modifications
are supported. Subsequently, a series of related research
works on PCH emerged, including accountable PCH [13],
revocable PCH [14] and multi-authority PCH [15], [16].
In addition, Jia et al. [17] introduced stateful Chameleon
Hash with Revocable Subkey (sCHRS) which has two kinds
of trapdoors: subordinate private key and master key. The
subordinate private key can be revoked by the master
key. By dividing transactions into standard parts and arbi-
trary parts, transaction field-level modifications are enabled.
Unfortunately, all these solutions rely on trusted (semi-
trusted) third parties which do not exist in the permissionless
setting.
Puddu et al. [18] also presented a redactable blockchain,
called µchain. The sender of a transaction can send some
different versions of the transaction, named “mutations”, and
establish a mutation policy, which defines who can and when
to trigger the mutations. µchain encrypts all mutation transac-
tions and makes decryption keys available only for one version
using dynamic proactive secret sharing (DPSS). Their solution
is unsuitable for the permissionless blockchain due to the
significant communication overhead of DPSS.
Deuber et al. [19] first proposed a redactable permissionless
blockchain protocol that required neither heavy cryptographic
primitives nor additional trust assumptions. Their protocol
utilized on-chain voting to reach an agreement on redaction.
Anyone can propose an edit request and the edit request is
only performed if more than φ > 1/2 fraction of votes in
ℓconsecutive blocks (voting period) after the edit request is
included in the chain. To account for redaction, they extend
the block header to accommodate an additional field called
old_merkle_root that stores the hash values of the replaced
transactions. Later, Thyagarajan et al. [20] proposed Reparo
which stores old_merkle_root in the repair layer database with-
out modifying the block structure of the immutable blockchain,
thus supporting repairability of existing contents (REC) on
the immutable blockchain. Despite their ingenious protocol,
there are two seemingly insurmountable problems. First, the
voting period ℓis very long. Assume that an adversary controls
ρ < 1/2 fraction of computational power in the network, and
the blockchain is fair [21] (i.e., the probability of a new block
generated by the adversary is also ρ), by Chernoff bound [22],
we have that P ≤e−(φ−ρ)2ℓ
φ+ρ , φ > ρ where P is the probability
of a malicious edit request is performed. In order to ensure P
is negligible, the voting period ℓshould be sufficiently large
(e.g., φ = 0.5, ρ = 0.4, P ≤2−20, the voting period ℓis
greater than 1802). Therefore, a single request has to wait
12.5 days to be effective for Bitcoin in the worst case, which is
completely impractical for deleting illegal data. Second, their
protocol can only cope with an adversary with ρ < 1/3 of the
total computational power for a real-world blockchain. Recall
that the original claim was ρ < 1/2 if the blockchain is fair.
Unfortunately, blockchains may be not fair due to some attacks
such as “selfish mining” proposed by Eyal [23]. They claim
that an adversary controlling only 1/3 of computational power
can violate the 1/2 threshold. Interestingly, this coincides with
an independent result by Garay et al. [24], where the fraction
of blocks for any ℓconsecutive blocks contributed by honest
players (aka. chain quality) is at least 1−
ρ
1−ρ . Therefore, one
has to assume that ρ < 1/3 to guarantee that φ > 1/2.
To remark, [19], [20] also considered transaction consis-
tency, however, in a limited sense: A redaction is allowed
only on a field that does not affect the state of the blockchain
(e.g., the OP_RETURN output scripts in Bitcoin). But illegal
content may still be inserted into other fields as shown in [3].
Moreover, their approaches cannot apply to the unspent trans-
action output (UTXO) model, which is easily seen as follows.
Suppose transaction tx1 is replaced by another one tx∗
1. Their
approaches keep the hash value H(tx1) in old_merkle_root
and validate the witnesses of tx1’s input scripts by using
H(tx1). However, a witness in Bitcoin only covers part of a
transaction that is determined by hash type, thus the witnesses
of tx1’s input scripts are not valid with respect to H(tx1).
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


## 第 3 页

LI et al.: WOLVERINE: A SCALABLE AND TRANSACTION-CONSISTENT REDACTABLE PERMISSIONLESS BLOCKCHAIN
1655
B. Technical Challenges and Solutions
In this paper, we explore the possibility of constructing
a permissionless redactable blockchain with scalability and
transaction consistency from a chameleon hash. In order to
defend against Sybil attack, we establish identities (i.e., pub-
lic/private keys) through a Sybil-resistant identity generation
mechanism such as [25], and run a decentralized bootstrapping
protocol [26] to agree on a group referred to as the regulatory
committee which holds the trapdoor key of chameleon hash.
When some data needs to be redacted, the committee members
find a collision in a fully distributed manner. Although this
idea seems simple, there are a few technical difficulties.
First, no efficient decentralized chameleon hash schemes are
available. Ateniese et al. [6] used robust secret sharing [27],
[28] to distribute the trapdoor key among the committee, and
an auxiliary MPC protocol [8], [9] to find collisions in the
chameleon hash. However, the large communication overhead
of the auxiliary MPC protocol leads to poor performance even
with a few hundred committee members. We solve this issue
by introducing a novel cryptographic tool, called t-out-of-n
non-interactive chameleon hash (NITCH) where each party
can compute its partial share in parallel without communi-
cating with other parties. Then each party publishes its share
on the blockchain such that anyone can find a collision by
collecting a set of t + 1 shares.
Second, the adversary may compromise the static committee
after a sufficiently long time. One approach to prevent this
attack is to refresh the committee (aka. committee evolution) in
every fixed interval of time known as an epoch. However, the
committee evolution is usually expensive due to the large over-
head of the Sybil-resistant identity generation mechanism [25]
and the decentralized bootstrapping protocol [26]. We solve
this issue by providing a generic transform from NITCH
to a decentralized random beacon (DRB) and designing a
committee evolution protocol based on the DRB. At the end
of every epoch, the old committee members run the DRB
protocol to generate a randomness string for the next epoch.
At the beginning of every epoch, all parties run a committee-
election protocol based on the randomness string to elect a new
committee and a distributed key resharing protocol is executed
between the old committee and the new committee to reshare
the trapdoor while keeping the trapdoor unchanged.
Third, the redaction should ensure transaction consistency,
however, most of the existing works [6], [11], [18] hap-
pened to ignore signature links between transactions. Recent
works [19], [20] considered transaction consistency, but
the solutions were unsuitable for a UTXO-type blockchain.
We solve this issue by introducing two new tools, namely
redaction policy and shadow transaction, guaranteeing that
a redaction will not break the signature links, and only
the admissible field (where harmful data can exist) can be
redacted, respectively.
C. Our Results
We introduce Wolverine, a transaction-consistent redactable
permissionless blockchain adapted to the UTXO model. Our
contributions are summarized as follows.
• Non-Interactive Threshold Chameleon Hash. As the main
tool towards data redaction, we introduce the notion of
non-interactive threshold chameleon hash (NITCH). We give
a formal model, security definitions, a concrete construction
based on bilinear pairings, and the corresponding security
proofs. Moreover, we note that NITCH is useful in building
DRB, a key component in our committee evolution protocol.
• A Stronger and Realistic Model. We first give a formal
model of transaction consistency based on weighted directed
acyclic graph (wDAG), namely, the redaction should retain
the signature links between transactions and should not
affect the state of the ledger. For simplicity, we limit our
scope to the UTXO model which has been widely adopted
by numerous blockchains.
• Construction. We present the construction of Wolverine
by using NITCH that satisfies the following properties:
(1) Wolverine can tolerate an adversary controlling ρ <
1/2 fraction of total computational power; (2) Wolverine is
scalable; (3) Wolverine satisfies transaction consistency; (4)
The redaction is instant and publicly verifiable.
• Implementation. We give a proof-of-concept implementa-
tion of Wolverine in Golang to demonstrate its practicality.
We leverage the fast polynomial evaluation and interpola-
tion [29] algorithm to speed up our NITCH implementation.
When a redaction needs to be performed, each party costs
only 0.57 ms to generate a vote. When the committee size is
2048, it only takes 0.1 s to combine the shares to find a colli-
sion. We also measure the additional overhead of Wolverine
compared with Bitcoin. The runtime of generating a block
of 1500 transactions is only 0.54 s, an increase of 0.18 s
compared with Bitcoin.
II. PRELIMINARY
In this section, we review some useful notations and notions.
Notations: We write a ←A(x) if a is assigned to the output
of algorithm A with input x. All algorithms are probabilistic
polynomial time (PPT), if not explicitly mentioned. We write
a ←A(x;r) if we make the random coins r explicit. Other-
wise, we assume that the random coins are drawn internally.
A. Number-Theoretic Assumption
A bilinear group generator G takes a security parameter κ
as input and outputs the descriptions of multiplicative groups
Λ = (G1, G2, GT , p, g1, g2, e) where G1, G2, GT are groups
of prime order p (κ-bit) with efficiently computable group
operations, and membership tests. The elements g1, g2 are the
generators of G1, G2, respectively. An efficiently computable
map e : G1 ×G2 →GT is called a bilinear map, if it satisfies:
(1) Bilinearity: for α, β ∈Zp, e(gα
1 , gβ
2 ) = e(g1, g2)αβ,
(2) Non-degeneration: e(g1, g2) ̸= 1. The computational co-
Diffie-Hellman (co-CDH) problem associated to Λ is to find
y = gαβ
2
when given g1, gα
1 , gβ
1 ∈G1 and g2, gα
2 ∈G2.
Definition 2.1 (co-CDH Assumption): ∀PPT adversary A,
∃a negligible function ν, s.t.,
Pr
"
y = gαβ
2

(α, β) ←$ Z2
p
y ←A(g1, gα
1 , gβ
1 , g2, gα
2 )
#
≤ν(κ)
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


## 第 4 页

1656
IEEE TRANSACTIONS ON INFORMATION FORENSICS AND SECURITY, VOL. 18, 2023
The probability is taken over the choice of (α, β) and A’s coin
tosses.
B. Decentralized Random Beacons (DRB)
A DRB [30] provides a way to collaboratively generate a
pseudorandom value without a centralized coordinator.
Definition 2.2 (DRB): A (t, n)-decentralized random bea-
con on a set of parties P = {P1, . . . , Pn} is specified as a
tuple of polynomial-time algorithms as follows:
• Setup(1κ): On input a security parameter κ, it outputs
public parameter pp.
• CmteGen(pp, t, n): This is an interactive protocol run by
the parties in P to set up a random beacon committee. The
protocol outputs a committee public key cpk and results in
a list of secret keys SK = {sk1, . . . , skn}, where each secret
key is only known to the corresponding party.
• PartialRand(cpk, ski, stℓ−1): On input a committee public
key cpk, a secret key ski, and a state stℓ−1 ∈Dom from
round ℓ−1, it computes a partial value σℓ,i and a proof
πℓ,i for round ℓ. Output is si = (i, σℓ,i, πℓ,i).
• CombRand(cpk, stℓ−1, E): On input a committee public
key cpk, a state stℓ−1 ∈Dom for round ℓ−1, and a set
E = {si}i∈I of shares from |I| ≥t + 1 parties, it outputs a
pair (σℓ, πℓ) of a beacon value and a proof, or ⊥.
• VerifyRand(cpk, stℓ−1, σℓ, πℓ): On input a committee pub-
lic key cpk, a state stℓ−1 ∈Dom, a beacon value σℓ∈Ran,
and a proof πℓ, it verifies if (σℓ, πℓ) is valid, and outputs a
decision d ∈{0, 1}.
• UpdState(cpk, stℓ−1, σℓ, πℓ): On input a committee public
key cpk, a state stℓ−1 ∈Dom, a beacon value σℓ∈Ran,
and a proof πℓ, it outputs the updated state stℓfor round ℓ,
or ⊥indicating unsuccessful.
Security of DRB: We reuse strong pseudorandomness first
considered in [30] (detailed definition given in Appendix in
supplementary material), which guarantees that the outputs of
DRB are indistinguishable from uniform randomness against
an active adversary, which implies the properties of unpre-
dictability and bias-resistance [31], [32].
C. Blockchain
1) Blockchain Basis: A block is a triple B = ⟨s, x, ctr⟩,
where s ∈{0, 1}κ is the hash of the previous block, x ∈
{0, 1}∗is the transaction data, and ctr ∈N is a nonce.
A blockchain is a chain (or sequence) of blocks denoted
by C. The rightmost block is called the head of the chain,
denoted by Head(C). Any chain C with a head Head(C) :=
⟨s, x, ctr⟩can be extended to a new longer chain C′ := C||B′
by attaching a (valid) block B′ := ⟨s′, x′, ctr′⟩such that
s′ = H(ctr, G(s, x)). A chain C can be empty denoted by
C = ε. The function len(C) denotes the length of a chain C
(i.e. number of blocks). The function extract(C) denotes the
sequence of transactions contained in chain C. For a chain
C of length n and any q ≥0, we denote by C⌈q the chain
resulting from removing the q rightmost blocks of C, and
analogously we denote by q⌉C the chain resulting in removing
the q leftmost blocks of C; note that if q ≥len(C), C⌈q := ε
and q⌉C := ε. If C is a prefix of C′ we write C ≺C′.
2) Blockchain
Protocol:
We
consider
an
immutable
blockchain protocol (for instance [24]) denoted by 5 that is
executed by an arbitrary number of parties, who interact with
each other to agree on a chain C. Characterized by global
parameters and validation rules, 5 consists of the following
algorithms:
• {0, 1} ←5.validateChain(C). On input a chain C, it returns
1 iff the chain is valid according to the set of validation rules.
• C ←5.compareChain({C1, . . . , Cn}). On input a set of
chains, it outputs the longest chain.
• C′ ←5.extendChain(C, x). On input a chain C, and a
sequence of transactions x, it attempts to extend a longer
chain C′ via solving a PoW puzzle.
• 5.broadcast(x). On input data x, it broadcasts it to all the
parties of the system.
3) Blockchain Security: As stated in the literature [24], [33],
a blockchain should satisfy persistence and liveness.
Definition 2.3 (Persistence [24]): Parameterized by k ∈N.
If in a certain round an honest party reports a ledger that
contains a transaction tx in a block more than k blocks deep,
then tx will be reported by any honest party in the same
position in the ledger, from this round on.
Definition 2.4 (Liveness [24]): Parameterized by u, k ∈N.
If a transaction is given as input to all honest parties contin-
uously for u consecutive rounds, then all honest parties will
report this transaction more than k blocks deep in the ledger.
III. OVERVIEW OF WOLVERINE
A. System Model
Consider a peer-to-peer network with N parties. We assume
that there is a regulatory committee R of size n ≪N whose
majority are honest parties. A trapdoor key of chameleon hash
is secretly shared among the committee. We start from scratch,
namely, bootstrapping the committee-based blockchain with-
out PKI setup. First, all parties establish identities (i.e., pub-
lic/private keys) through a Sybil-resistant identity generation
mechanism such as [25]. Then, a decentralized bootstrapping
protocol [26] is performed to agree on a committee of size n.
Although this incurs a significant communication overhead,
it only executes once.
Anyone in the network can initiate a redaction instruction
(RI) by including the RI into a block. The committee members
validate the RI according to a redaction policy (RP). If the
RI satisfies RP, the committee members generate votes and
broadcast them as transactions into the network. The miner
checks the votes and packs the valid votes into the next block.
When the votes originating from different committee members
are more than n/2, each party locally combines these shares
to find a collision in the chameleon hash, executes the RI and
updates its local chain.
To prevent the committee from possible adversarial takeover
as some members may leave the network or be cor-
rupted by the adversary, a new committee is re-elected
at the beginning of each epoch and the trapdoor key
of the chameleon hash is redistributed among the new
committee.
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


## 第 5 页

LI et al.: WOLVERINE: A SCALABLE AND TRANSACTION-CONSISTENT REDACTABLE PERMISSIONLESS BLOCKCHAIN
1657
B. Network Model and Threat Model
We assume the network to be synchronous, which is widely
adopted by public blockchain protocols [24], [26], [34], [35].
In particular, we assume that the honest parties are connected
and the communication channel between honest parties is
synchronous, i.e., a message sent from an honest party to other
honest parties will be received with a delay at most δ. For the
committee evolution protocol, we further assume there is a
synchronous broadcast channel between parties. Broadcast is
achievable in practice using Byzantine agreement. Recently,
Ren et al. [36] proposed an expected constant-round algo-
rithm for Byzantine consensus in a synchronous, authenticated
communication network, where less than half of the parties
can be corrupt. However, significant communication overhead
makes the approach impossible when the number of parties
is large. We have to find an alternative solution. Fortunately,
a blockchain itself can be seen as a bulletin board [25], [37],
[38], [39], [40], where parties can either post a message (or)
retrieve any number of messages from the blockchain. After a
party posts a message to the blockchain, within a finite time
period u (the liveness parameter of the blockchain), it gets
published and is retrievable by other parties. We stress that
by redaction policy defined in this paper, redactabililty to the
blockchain only applies to certain entries and thus does not
affect other valid data on-chain, hence the assumption of using
blockchain itself as a broadcast channel is reasonable and
realistic if a redactable blockchain meets chain growth and
chain quality (as a basic requirement in [24]), and redactable
common prefix (i.e., any valid message on the blockchain is
not modified), and the majority of the committee are honest.
We consider a probabilistic polynomial-time Byzantine
adversary who corrupts ρN parties at any time with ρ < 1/2.
The corrupt parties can deviate from the protocol in any
arbitrary manner, e.g., by sending invalid or inconsistent mes-
sages, or remaining silent. Similar to most committee-based
blockchain protocols [26], [35], we assume the adversary to
be slowly adaptive, namely, it is allowed to select the set of
corrupt parties at the beginning of or during an epoch, but the
corruption will not be effective unless the epoch ends.
C. Security Goals
First, we require that Wolverine satisfies transaction con-
sistency. For a formal definition, we simplify the transaction
into a tuple (in, out, wit) similar to [41]. Let f : dom( f ) →
ran( f ) denote a mapping f from domain dom( f ) to range
ran( f ). Let H(tx) denote the ID of transaction tx where H
is a collision-resistant hash function.
Definition 3.1 (Transactions with UTXO): A transaction tx
is a tuple (in, out, wit), where:
• tx.in : N →txid × N.
• tx.out : N →script × N.
• tx.wit : N →script, where dom(wit) = dom(in).
for all i, j ∈dom(in) and i ̸= j, it satisfies that in(i) ̸= in( j).
The fields in and out represent the inputs and outputs of
a transaction, respectively. tx.in(i) = (txid, j) means that
the i-th input of the transaction tx wants to redeem the j-th
output of the transaction tx′ with H(tx′) = txid. The output
Fig. 2.
Weighted directed acyclic graph of a transaction.
tx′.out( j) is a pair (script, v) where script is a challenge and
v is the amount of currency, and anyone who can provide a
witness satisfying the challenge script can redeem the output.
Such a witness is defined by tx.wit(i).
The transactions in a blockchain C constitute a weighted
directed acyclic graph denoted by wDAG(C). As shown
in Fig. 2, the outputs identified by the inputs of the transaction
are the predecessors of the outputs of the transaction, and the
witnesses are the edge between them. Each edge has a weight
that is assigned according to some specific rule and satisfies
that the sum of the weights of all the in-edges of a vertex (an
output) is equal to its amount of currency (e.g., v11+v21 = v1).
The vertices with the out-degree of 0 represent the unspent
transaction outputs of the ledger C.
Intuitively, transaction consistency requires that wDAG(C1)
and wDAG(C2) are equal where C2 is the result of revision
of C1. However, some malicious data may be inserted into
the outputs as challenge scripts such that the outputs can
never be spent (e.g., malicious data as public key, public
key hash, or script hash in Bitcoin). Therefore, we have to
allow the redaction can alter the challenge scripts of the
unspent outputs. For two outputs out1 = (script1, v1) and
out2 = (script2, v2), if the witness of script1 satisfies the
challenge script2 and v1 = v2, we define out1 = out2.
Let G1 = G2 denote the graphs G1 and G2 are identical,
G1 ∼= G2 denote the graphs G1 and G2 are isomorphic, and
G −O denote the graph that results by deleting the vertices
and the edges adjacent to the vertices included in O from the
graph G where O is a subset of the vertices in the graph G.
The formal definition of transaction consistency is below.
Definition 3.2 (Transaction Consistency): The ledgers C1,
C2 owned by two honest parties at the onset of the rounds
r1 ≤r2 of length l1 and l2, respectively, satisfy the following
condition:
G1 −O1 = G2 −O2 and G1 ∼= G2
where G1 = wDAG(C⌈k
1 ), G2 = wDAG(C⌈l2−l1+k
2
), k ∈N
denotes the persistence parameter, and O1, O2 is the set of
vertices with out-degree of 0 in G1, G2, respectively.
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


## 第 6 页

1658
IEEE TRANSACTIONS ON INFORMATION FORENSICS AND SECURITY, VOL. 18, 2023
Second, a blockchain protocol should satisfy chain growth,
chain quality and common prefix properties to ensure the
ledger are persistent and live.
Definition 3.3 (Chain Growth [24]): The
chain
growth
property Qcg with parameters τ ∈R and s ∈N states that
for any honest party P with a chain C, it
holds that after
any s consecutive rounds it adopts a chain that is at least
τ · s blocks longer than C.
Definition 3.4 (Chain Quality [24]): The
chain
quality
property Qcq with parameter ℓ∈N and µ ∈(0, 1) states
that for any honest party P with chain C, the ratio of honest
blocks in any ℓconsecutive blocks of C is at least µ.
Definition 3.5 (Common Prefix [24]): The common prefix
property Qcp with parameter k ∈N states that the chains
C1, C2 possessed by two parties at rounds r1 ≤r2 respectively,
it holds that C⌈k
1 ≺C2.
However,
the
notion
of
the
common
prefix
targets
immutable blockchains. In order to support transaction filed
level redaction in the chain, we propose a new definition
named redactable common prefix. Before giving the formal
definition of the redactable common prefix, we present a
redaction policy RP, that determines if a redaction instruction
RI to a chain C is valid. Let C∗←Redact(C, RI) denote the
chain C∗generated by executing the redaction instruction RI
on the chain C.
Definition 3.6 (Redaction policy): For a ledger C in round
r, a redaction instruction RI is said to satisfy the redaction
policy RP, i.e., RP(C, RI) = 1, if it holds that V (xC∗) =
1 where V is the transaction validation predicate, C∗←
Redact(C, RI) and xC∗= extract(C∗).
The transaction validation predicate V takes as input a
transactions xC of a chain C, and outputs 1 iff the transactions
are valid. According to V , one can instantiate either Bitcoin
as well as other cryptocurrencies. We do not give a specific
definition of V but list two basic requirements based on the
UTXO model. For any transaction tx = (in, out, wit):
• R1: The amount of currency for all outputs tx.out is less
than amount of currency for all inputs tx.in.
• R2: The witness tx.wit[i] satisfies the challenge script of
the output identified by tx.in[i].
Let B∗←Redact(B, RIs) be the block generated by
executing the set RIs of redaction instructions on block B.
Informally, redactable common prefix states that if a redaction
instruction is performed on the chain of an honest party, then
the redaction instruction must satisfy RP and be approved by
the committee. In addition, the redaction instruction will be
performed by all honest parties. A formal definition follows.
Definition 3.7 (Redactable Common Prefix): The redactable
common prefix property Qrcp with parameter k ∈N states
that the chains C1, C2 of length ℓ1, ℓ2 possessed by two honest
parties at rounds r1 ≤r2 respectively, it holds:
• if l1 = l2, C⌈k
1 ≺C2;
• if l1 < l2, C⌈k
1
≺C2 or for each B j
∈C⌈k
1
and
B∗
j ∈C⌈(ℓ2−ℓ1)+k
2
at the same position j ∈[l1 −k]
of the ledger C1 and C2 respectively, if B j
̸=
B∗
j ,
it must exist a set RIs of redaction instructions such
that B∗
j ←Redact(B j, RIs), and for each RI ∈RIs,
RP(C2, RI) = 1 and P2 received at least n/2 + 1 valid
votes for RI.
where k is the persistence parameter.
IV. NITCH
In this section, we present the model of non-interactive
chameleon hash, a pairing-based construction, and the security
proofs in the random oracle (RO) model.
A. Model of NITCH
Definition 4.1 (NITCH): A
t-out-of-n
non-interactive
threshold chameleon hash scheme NITCH with message
space M on a set of parties P = {P1, . . . , Pn} contains of a
tuple of polynomial-time algorithms as follows:
• Setup(1κ). On input a security parameter κ, this algorithm
outputs public parameters pp.
• DistKG(pp, t, n). This is an interactive protocol run by the
parties in P. The protocol outputs a global public key pk,
a list SK = {sk1, . . . , skn} of parties’ secret keys where
each secret key is only known to the corresponding party,
and a list VK = {vk1, . . . , vkn} of parties’ verification keys.
• Hash(pk, m). On input a public key pk and a message m ∈
M, this algorithm outputs a pair (h,r) of a hash value and
a check value.
• Verify(pk, h, m,r). On input a public key pk, a message
m ∈M, a hash value h and a check value r, this algorithm
verifies if (h, m,r) is valid and outputs a decision d ∈{0, 1}.
• PartialAdapt(ski, vki, m, m′). On input a secret key ski, the
corresponding verification key vki, an old message m ∈M
and a new message m′ ∈M, this algorithm computes an
adaptation share ci and a proof πi. Output is a check value
share si = (i, ci, πi)
• PartialVerify(pk, vki, si, m, m′). On input a public key pk,
a verification key vki, a check value share si and a message
pair m, m′ ∈M, this algorithm verifies if si is valid and
outputs a decision d ∈{0, 1}.
• Combine(VK, (h, m,r), m′, E). On input a list of verifica-
tion key VK, a valid tuple (h, m,r), a message m′ ∈M and
a set E = {si1, . . . , si|I|} of check value shares originating
from |I| ≥t + 1 different parties, this algorithm outputs a
new check value r′, or ⊥.
Correctness: For all κ ∈N, pp ←Setup(1κ), (QUAL,
pk, SK, VK) ←DistKG(pp, t, n), for all m, m′
∈M,
(h,r) ←Hash(pk, m), for any subset U ⊂QUAL of size
at least t + 1, it holds that r′ ←Combine(VK, (h, m,r), m′,
{si}i∈U) and Verify(pk, h, m,r) = Verify(pk, h, m′,r′) = 1,
where si ←PartialAdapt(ski, vki, (h, m,r), m′), i ∈U.
We assume synchronous communication model as in [42],
where the parties are connected by a complete network of
private point-to-point channels, and all parties have access to
a broadcast channel. The communication model is required by
the DistKG protocol which only runs once after the committee
R is determined in Wolverine. The broadcast channel can be
obtained through [25]. We also assume that a static adversary
can corrupt up to t < n/2 parties. This assumption does not
contradict the slowly adaptive adversarial model of Wolverine
as the slowly adaptive adversary is static in each epoch.
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


## 第 7 页

LI et al.: WOLVERINE: A SCALABLE AND TRANSACTION-CONSISTENT REDACTABLE PERMISSIONLESS BLOCKCHAIN
1659
In Wolverine, we require that an adversary cannot modify
a transaction filed without the approval of the committee even
after seeing arbitrary modifications to other transaction fields.
Here we need collision-resistance in a distributed manner such
that A should not find a collision for the challenge message
m∗, even if A controls θ parties (θ ≤t), adaptively acquires up
to a polynomial number of collisions other than the challenge
message m∗, and queries up to t −θ honest parties on m∗.
Definition 4.2 (Collision-Resistance): A
NITCH
scheme
over a set of parties P = {P1, . . . , Pn} meets (t, n)-collision-
resistance with 0 ≤t < n/2, if ∀PPT adversary A, ∃a
negligible function ν, s.t.,
Pr[ExpCollRes
A,NITCH(1κ) = 1] ≤ν(κ)
where ExpCollRes
A,NITCH(1κ) is defined in the following game.
Corruption: A chooses a collection C of parties to be
corrupted with |C| ≤t. A acts on behalf of the corrupted
parties, while the challenger acts on behalf of the remaining
honest parties.
Initialization: The challenger and A engage in the protocol
DistKG that establishes a qualified set of parties QUAL ⊆
P. Every (honest) party Pi ∈QUAL\C obtains a key pair
(ski, vki), while corrupted nodes N j ∈C end up with key
pairs (sk j, vk j) (possibly undefined). The global public key
pk and the verification key vector VK are known to both
the challenger and A.
Query: In response to A’s adaption query (i, m, m′) for
an honest party Pi ∈QUAL \ C and a message pair
(m, m′)
∈
M × M, the challenger responds si
←
PartialAdapt(ski, vki, m, m′). In any other case, the chal-
lenger returns ⊥. A can invoke a polynomial number of
adaption queries.
Output: A outputs (m∗,r∗, m′∗,r′∗, h∗). The experiment
outputs 1 if the following conditions hold: 1) |C| + |S| <
t + 1, where S ⊂QUAL \ C is the subset of honest
parties for which A made an adaption query of the
form (i, ·, m∗) or (i, m∗, ·); 2) Verify(pk, h∗, m∗,r∗) =
Verify(pk, h∗, m′∗,r′∗) = 1.
Moreover, we expect that any redaction can be performed
with the approval of the committee even in the presence
of an adversary that is allowed to corrupt up to t parties.
In particular, if the Combine algorithm does not return ⊥,
its output must pass the Verify algorithm even in the presence
of the adversary’s inputs to the Combine algorithm.
Definition 4.3 (Robustness): A
NITCH
scheme
satisfies
(t, n)-robustness if ∀PPT A, ∃a negligible function ν, s.t.,
Pr[ExpRobustness
A,NITCH (1κ) = 1] ≤ν(κ)
where ExpRobustness
A,NITCH (1κ) is defined in the following game:
Corruption, Initialization and Query: These phases are
defined exactly as collision-resistance in Definition 4.2.
Challenge: The challenger receives from A a set U ⊂QUAL
of size at least t + 1, a valid tuple (h, m,r), a message
m′ ∈M and a set of check value shares {si}i∈U∩C. The
challenger proceeds to compute the check value shares
s j ←PartialAdapt(sk j, vk j, m, m′) for j ∈U\C. Let
r′ ←Combine(VK, (h, m,r), m′, {si}i∈U). If r′ ̸= ⊥and
Verify(pk, h, m′,r′) = 0, output 1; else, output 0;
Since hiding whether a transaction has been modified is out
of our scope, indistinguishability [43] is omitted here, we only
point it out yet without further discussion that our scheme
achieves this against an outsider distinguisher.
B. NITCH Instantiation
We now give a direct construction in type-III bilinear
groups [44] where G1 ̸= G2 and there is no efficiently
computable isomorphism between G1 and G2. In the con-
struction, we employ the communication-efficient Pedersen’s
distributed key generation (DKG) protocol [45] where a single
communication round is only needed when the parties follow
the protocol. However, the DKG protocol cannot ensure that
the public key is uniformly distributed, and is not simulatable.
Therefore, we prove the security of our construction under
the random oracle model by direct reducing to the underly-
ing co-CDH assumption, not by reducing to the centralized
chameleon hash scheme using the concept of simulatable
adversary view [46]. To answer the adaption oracle queries on
challenge message m∗when the secret key ski is unknown,
we use the non-interactive zero knowledge (NIZK) proof
of equality of discrete logarithms [47] instead of pairing
equations (as used in a BLS signature) to verify a DH-tuple.
The technique proposed in [30] also comes with two other
benefits. First, it speeds up our construction due to the NIZKs
need only 4 exponentiation operations in G1 while pairing
equations need 2 pairings which are more computationally
expensive. Second, it reduces the size of parameters by placing
verification keys and the check values on the group G1.
Construction 1 (Pairing-based NITCH): Let H1 :{0, 1}∗→
G1, H2 : {0, 1}∗→Zp are hash functions modeled as a ran-
dom oracle in the security analysis. The detailed description
of the pairing-based NITCH is as follows:
• Setup(1κ): The algorithm outputs Λ ←G(1κ) as public
parameters pp, where G is a bilinear group generator.
• DistKG(pp, t, n): The distributed protocol is a variant of
Pedersen’s DKG protocol that is desceibed in Protocol 1.
• Hash(pk, m): The algorithm randomly picks a check value
r
$
←−G1, and outputs h ←e(r, g2)e(H1(m), pk).
• Verify(pk, h, m,r): The algorithm outputs 1 iff the equality
h = e(r, g2)e(H1(m), pk) holds and 0 otherwise.
• PartialAdapt(ski, vki, m, m′): The algorithm outputs an
adaption share si = (i, ci, πi) as follows:
1) Calculate ci ←yski where y := H1(m)H1(m′)−1;
2) Randomly sample τ
$
←−Zp, set ω ←H2(g1, y, vki,
ci, gτ
1, yτ), and compute z ←τ + ω · ski. The NIZK
proof is π = (ω, z).
• PartialVerify(pk, vki, si, m, m′): The algorithm parses si =
(i, ci, π = (ω, z)) and outputs 1 iff ω = H2(g1, y, vki, ci,
gz
1/vkω
i , yz/cω
i ) holds where y := H1(m)H1(m′)−1.
• Combine(VK, (h, m,r), m′, E): The algorithm parses the
list E = {si1, . . . , si|E|} of |E| ≥t + 1 candidate adaptation
shares originating from |E| different parties, and obtains
verification keys vki1, . . . , vki|E|. Next,
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


## 第 8 页

1660
IEEE TRANSACTIONS ON INFORMATION FORENSICS AND SECURITY, VOL. 18, 2023
Protocol 1 DistKG protocol
Dealing: Each party Pi choose a random polynomial fi(z) =
ai,0 + ai,1z + · · · + ai,tzt over Zp of degree t. Let xi = ai,0 =
fi(0). Pi broadcasts Ai,k = gai,k
1
for k = 0, . . . , t and Bi,0 =
gai,0
2
. Pi computes the shares si, j = fi( j) for j = 1, . . . , n
and sends si, j to Pj.
Verification: Each party Pj verifies the shares received from
the other parties. For each i = 1, . . . , n, Pj checks if
g
si, j
1
=
tY
k=0
(Ai,k) jk
and
e(g1, Bi,0) = e(Ai,0, g2)
(1)
If the check fails for an index i, the party Pj broadcasts a
complaint against the party Pi.
Complaint: Any party who received strictly more than t
complaints is immediately disqualified. Each party Pi who
received a complaint from another player Pj responds by
returning the correct shares si, j. If any of these new shares
does not satisfy Eq. (1), Pi is disqualified. Let QUAL ⊆
{1, . . . , n} be the set of non-disqualified players.
Extraction: The public key is pk = Q
i∈QUAL Bi,0. The public
verification keys are computed as vk j = Q
i∈QUAL
Qt
k=0 A jk
i,k
for j ∈QUAL. Each party Pj locally computes his private
key share sk j = P
i∈QUAL si, j.
1) Identifies a subset I
=
{ j1, . . . , jt+1} of size t +
1 such that for every i ∈I, ω = H2(g1, y, vki, ci,
gz
1/vkω
i , yz/cω
i ) holds where y := H1(m)H1(m′)−1. If no
such subset exists, outputs ⊥.
2) Outputs r′ ←r Q
i∈I c
ℓI
i (0)
i
by Lagrange interpolation in
the exponent where ℓI
i (0) is the Lagrange coefficients.
Theorem 1: The NITCH scheme (Construction 1) meets
collision-resistance and robustness in the random oracle (RO)
model, if the co-CDH assumption holds.
We postpone the complete proof to Appendix (cf. suppli-
mentary material).
C. Constructing DRB From NITCH
We now give a DRB construction from NITCH that is the
essential component of the committee evolution protocol.
Construction 2 (NITCH-to-DRB): Let H : R →Ran be a
hash function where R is the NITCH’s check value space.
• Setup(1κ): The algorithm runs Λ ←NITCH.Setup(1κ)
and outputs pp = (Λ, m0,r0) where m0
$
←−Dom and r0
$
←−
R.
• CmteGen(pp, t, n): A group of n parties P = {P1, . . . , Pn}
jointly run (pk, SK, VK) ←DistKG(Λ, t, n). The com-
mittee public key cpk
=
(pk, VK, h0) where h0
←
Hash(pk, m0;r0).
• PartialRand(cpk, ski, stℓ−1): The algorithm parses the
committee public key cpk = (pk, VK, h0), and outputs
(σi, πi) ←PartialAdapt(ski, vki, m0, stℓ−1).
• CombRand(cpk, stℓ−1, E): The algorithm parses the com-
mittee public key cpk = (pk, VK, h0), and computes π ←
Combine(VK, (h0, m0,r0), stℓ−1, E), σ ←H(π). Output
is (σ, π).
Fig. 3.
Merkle root of a block containing 4 transactions.
• VerifyRand(cpk, stℓ−1, σℓ, πℓ): The algorithm parses the
committee public key cpk = (pk, VK, h0), and computes
b ←Verify(pk, h0, stℓ−1, π). Output is b.
• UpdState(cpk, stℓ−1, σℓ, πℓ): The algorithm outputs the
new state stℓ= σℓ∥ℓfor the next round ℓ.
The round number is of ϱ-bit and the total number of rounds
is at most 2ϱ. The state st0 = σ0∥0 where σ0 is left open.
Theorem 2 (Strong Pseudorandomness): If the NITCH sat-
isfies collision-resistance, the DRB protocol in Construction 2
is strong pseudorandom in the RO model.
We postpone the complete proof to Appendix (cf. suppli-
mentary material).
V. CONSTRUCTION OF WOLVERINE
In the section, we detail the building-blocks in Wolverine.
A. Shadow Transaction
We introduce the concept of shadow transactions to enable
the redaction on transaction field level. We regard the fields of
a transaction, which can be inserted arbitrary data, as admis-
sible fields denoted by AF (tx.out.script and tx.wit), and
regard the remaining fields as stable fields denoted by SF
(tx.in, tx.out.v). As shown in Fig. 3, a shadow transaction stx
is a copy of the origin transaction tx, except that the admissible
fields are replaced with the corresponding NITCH values.
To calculate the Merkle root of the transactions in a block,
we replace the hash H(tx) with H(stx) for each transaction
tx. Therefore, the shadow transaction brings two properties:
(1) redactabililty: the admissible fields of a transaction can
be modified by the committee R; (2) immutability: the stable
fields of a transaction is immutable if H is collision-resistant.
Moreover, we define the transaction ID of a transaction tx as
txid = H(stx) to ensure that the txid is unchanged even if
the admissible fields of the transaction is redacted.
In order to generate the shadow transaction, a random
check value is needed to calculate the NITCH value for
every admissible field. However, the random check values
not only increase the storage overhead of the blockchain, but
also provide a new way to insert illegal data. Fortunately,
we can use the same check value for all admissible fields of all
transactions (e.g., the generator g1 of group G1) as the check
values do not affect the collision-resistance in Construction 1.
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


## 第 9 页

LI et al.: WOLVERINE: A SCALABLE AND TRANSACTION-CONSISTENT REDACTABLE PERMISSIONLESS BLOCKCHAIN
1661
Protocol 2 Redactable blockchain protocol 5RB
Updating local chain: At the beginning of a new round, the
parties maintain a chain C′ of length l′ and receive a set of
chain. They then update their local chain with a chain C of
length l > l′ by calling 5RB.compareChain.
Collecting
redaction
instruction:
Collect
all
redaction
instructions RIs from the chain l′−k⌉C, For each RI ∈RIs:
• (Add
the
redaction
instruction
into
RIP).
If 5RB.validateRI(C, RI) = 0 or RI targets a transaction
field that contains a valid vote, discard RI, otherwise
add (RI, ∅) to the pool RIP.
• (Voting for the redaction instruction). If the committee
member is willing to perform RI added to the RIP, call
5RB.generateVote to generate a vote, insert it into a
transaction, and broadcast the transaction to the network.
Then update the record (RI, ∅) to (RI, {vote}) in the pool
RIP.
Collecting votes: Collect all votes from the chain l′−k⌉C. For
each vote ⟨H(RI), si⟩, retrieve (RI, shares) from RIP, and
update the record to (RI, shares ∪si) if si /∈shares and
si is valid.
Modifying transactions: For each (RI, shares)
∈
RIP,
if len(shares) > n/2, parse the redaction instruction RI as
(txid, i, x∗), call NITCH.Combine algorithm to generate a
new check value r′ and add ((txid, i),r′) to CVRL. Then,
replace the admissible field identified by (txid, i) with x∗.
Extending chain: Create a new block B = ⟨s, x, ctr, h⟩,
such that s = H(ctr′, G(s′, x′, h′)) for ⟨s′, x′, ctr′, h′⟩←
Head(C) and h = H(CVRL). Then extend local chain C′ ←
C||B and broadcast C′ to the network.
Propose a redaction instruction: Any user can propose
a redaction instruction RI = (txid, i, x∗), insert it into a
transaction, and broadcast the transaction to the network by
calling 5RB.broadcast.
B. The Redactable Blockchain Protocol
We assume that there exists a committee R of size n
with honest majority. The committee can be obtained by the
decentralized bootstrapping protocol [26] in the bootstrapping
phase or by the committee evolution protocol when the epoch
changes. We build the redactable blockchain protocol 5RB by
extending the immutable blockchain protocol 5. Besides all
the algorithms of 5 (ref to Section II-C.2), 5RB provides two
additional algorithms.
• {0, 1} ←5RB.validateRI(C, RI): On input a chain C,
a redaction instruction RI, it returns 1 iff RP(C, RI) = 1.
• vote ←5RB.generateVote(C, RI, sk j, vk j): On input a
chain C, a redaction instruction RI = (txid, i, x∗), a secret
key share sk j and a verification key vk j, it outputs a
vote ⟨H(RI), s j⟩where s j ←NITCH.PartialAdapt(sk j,
vk j, x, x∗) and x is the data identified by (txid, i).
We also introduce two additional local state databases.
Redaction instruction pool (RIP) which stores a list of pair
(RI, votes) where RI is the form of (txid, i, x∗) that means
to replace the i-th admissible field of a transaction whose
transaction ID is txid with the data x∗, and votes is a
collection of valid votes on the redaction instruction RI.
Check value revocation list (CVRL) which stores a list of pair
((txid, i),r′) where r′ is the latest check value for the field
identified by (txid, i). To ensure the views of CVRL for all
(honest) parties are the same, we extend the block header as
B := ⟨s, x, ctr, h⟩where h = H(CVRL).
During the setup of the redactable blockchain protocol
5RB, the committee runs NITCH.DistKG to generate the
trapdoor key gsk, and chooses parameters m0,r0, σ0, which
are public, for the DRB protocol, and all parties maintain
a local state consisting of a chain C = genesis, an empty
RIP = {} and an empty CVRL = {}. The redactable blockchain
protocol 5RB runs in a sequence of rounds starting with 1,
and the sequence of execution for each round is described as
Protocol 2.
C. The Committee Evolution Protocol
Protocol 3 depicts our committee evolution protocol which
consists of three main components: randomness generation,
committee election, and distributed key resharing. At the end
of epoch e, the committee R(e) runs the DRB protocol to
generate a randomness string for next epoch e + 1. At
the
beginning of epoch e + 1, all parties run a committee election
protocol to agree on a new committee R(e+1). Finally, a dis-
tributed key resharing (DKRS) protocol is run between R(e)
and R(e+1) to secretly reshare the global private key gsk to
R(e+1). The DRB protocol is described in Section IV-C. In the
following, we explain the committee election protocol and the
DKRS protocol in more details.
1) Committee Election: The committee election protocol
relies on PoW to protect against Sybil attacks by requiring
every party who wants to join in the committee to solve a
PoW puzzle. The committee election protocol is parameterized
by the following parameters: (i) ρ is the fraction of parties
controlled by the adversary in the network; (ii) D is the
difficulty level that determines how much work a processor
has to do to solve a PoW puzzle; (iii) 1 is the protocol
running time. To solve the inconsistencies among the honest
parties’ views of who are in the committee, we capitalize
the blockchain as a public bulletin board and require that all
parties publish their identities and corresponding PoW solution
on the blockchain.
2) DKRS: The DKRS protocol is a variant of Pedersen’s
DKG protocol, where the global secret key gsk is (t(e), n(e))-
secretly shared among the members of R(e). The main idea of
DKRS protocol is that each party P(e)
i
runs a (t(e+1), n(e+1))-
verifiable secret share (VSS) of Feldman [48] to distribute its
secret key ski among the committee R(e+1). Then each party
P(e+1)
j
in the committee R(e+1) locally computes its secret key
sk(e+1)
j
= P
i∈U(ℓU
i (0)·si, j) where U ⊆R(e) with |U| > t(e),
ℓU
i
is the Lagrange coefficient and si, j is the share received
from Pi ∈U. For any set T ⊆R(e+1) with |T | > t(e+1),
P
j∈T (ℓT
j (0)·sk(e+1)
j
) = P
i∈U(ℓU
i (0)·P
j∈T (ℓT
j (0)·si, j)) =
P
i∈U(ℓU
i (0) · sk(e)
i ) = gsk.
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


## 第 10 页

1662
IEEE TRANSACTIONS ON INFORMATION FORENSICS AND SECURITY, VOL. 18, 2023
Protocol 3 Committee Evolution
(1) Randomness generation during epoch e
(a) The committee R(e) runs the DRB protocol to generate
a random string τ (e+1) for the next epoch.
(b) The committee R(e) reveals τ (e+1) on the blockchain at
the end of epoch e.
(2) Committee election for epoch e + 1
(a) At the beginning of epoch e + 1, each party locally
chooses a public key PK and sets (PK, IP) as its
identity. The public key PK and the IP address IP are
used to establish authenticated communication for the
DKRS protocol.
(b) Each party, who finds a PoW solution nonce such
that H(τ (e+1)||IP||PK||nonce) ≤2κ−D, publishes the
solution and his identity on the blockchain.
(c) After 1 time, the protocol stops and assigns each iden-
tity, who finds a valid PoW solution, to the committee
R(e+1).
(3) Distributed Key ReSharing
(a) Dealing phase: Each party P(e)
i
in R(e) chooses a
random polynomial
fi(z) = sk(e)
i
+ ai,1z + · · · +
ai,t(e+1)zt(e+1) over Zp of degree t(e+1), where sk(e)
i
is
the party Pi’s secret share at epoch e and t(e+1) is the
threshold at epoch e+1. P(e)
i
broadcasts Ai,k = gai,k
1
for
k = 0, . . . , t(e+1). P(e)
i
computes the shares si, j = fi( j)
for j = 1, . . . , n(e+1) and sends si, j to player P(e+1)
j
in
R(e+1).
(b) Verification round: Each party P(e+1)
j
in R(e+1) verifies
the shares received from the others. For each i =
1, . . . , n(e+1), P(e+1)
j
checks if
g
si, j
1
=
t(e+1)
Y
k=0
(Ai,k) jk
(2)
If the check fails for an index i, the party P(e+1)
j
broadcasts a complaint against the party P(e)
i
.
(c) Complaint phase: Any party who receives strictly more
than t(e+1) complaints is immediately disqualified. Each
party P(e)
i
who receives a complaint from a player
P(e+1)
j
responds by returning the correct shares si, j.
If any of these new shares does not satisfy Eq. (2), Pi is
disqualified. Let U ⊆R(e) be the set of non-disqualified
parties.
(d) Extraction phase: The public verification keys are com-
puted as vk j = Q
i∈U
Qt(e+1)
k=0 A
ℓU
i (0)· jk
i,k
for j ∈R(e+1).
Each party P(e+1)
j
locally computes his private key
share sk j = P
i∈U(ℓU
i (0) · si, j).
VI. SECURITY ANALYSIS OF WOLVERINE
A. Security of the Redactable Blockchain Protocol
The redaction is instant as all committee members can
generate votes in parallel and the votes will be published on the
blockchain within a finite time period u. The redaction is also
fully transparent and accountable as all votes can be publicly
verified by anyone. Assuming that there exists a committee
of n members with more than n/2 honest members and 5
satisfies chain growth, chain quality, and common prefix,
we now show that the redactable blockchain 5RB provides
all the security requirements defined in Section III-C.
Theorem 3: If 5 satisfies chain growth, chain quality, and
common prefix, and the NITCH scheme satisfies collision-
resistant, 5RB satisfies chain growth, chain quality, and
redactable common prefix for any adversary controlling less
than n/2 committee members.
Proof: We prove each property separately.
Chain growth: We note that in 5RB, it is impossible to
remove a block. The modification performed on the chain do
not alter the length of the chain. Therefore, 5RB satisfies chain
growth if 5 satisfies chain growth.
Chain quality: We define that an honest block modified
by the adversary without the approval of the committee
is malicious. Therefore, if an adversary corrupting up to
n/2 committee members can find a collision, the adversary
breaks the chain quality of 5RB. However, the probability is
negligible as it is equal to breaking the collision-resistance of
NITCH.
Redactable common prefix: Since the immutable blockchain
5 satisfies common prefix and all parties have the same view
of CVRL at block Bl1−k, it
must hold that C⌈k
1
≺C2 when
l1 = l2. When l1 < l2, if no modifications occur on C⌈l2−l1+k
2
,
then C⌈k
1 ≺C2. If P2 performed a redaction instruction RI on
the chain C⌈l2−l1+k
2
, then P2 received more than n/2 votes on
RI as P2 is honest. Therefore, at least one honest party in the
committee approved RI, i.e., the redaction instruction RI must
satisfy the redaction policy RP.
Theorem 4 (Transaction Consistency): If
the
NITCH
scheme
satisfies
collision-resistance
and
5RB
satisfies
redactable
common
prefix,
5RB
satisfies
transaction
consistency
for
any
adversary
controlling
less
than
n/2 committee members.
Proof:
For ledgers C1, C2 owned by two honest parties
P1, P2 at rounds r1 < r2 of length l1,l2, respectively, let
G1 = wDAG(C⌈k
1 ) and G2 = wDAG(C⌈l2−l1+k
2
). If there
are no modification occurs during rounds [r1 −k,r2], then
G1 = G2 due to the redactable common prefix property.
In 5RB, the addition and deletion of outputs are not allowed
and the stable fields of a transaction tx, including tx.in,
tx.out.v are immutable, thus the number of vertices in G1 and
G2 is equal, and the predecessors of each vertex and the weight
of each edge are unchanged. We next discuss the modifications
that occur on the challenge script and witnesses respectively.
If a modification occurs on a witness x, which redeems an
output (script, v), during rounds [r1−k,r2], then the modified
witness x∗must satisfy the challenge script script according
to the requirement R2 of V . Therefore, the graphs G1 and
G2 are isomorphic due to the modifications do not break the
edges of the graph.
If a modification occurs on the challenge script of an output
(script, v) during rounds [r1 −k,r2], then the output is either
not spent, or the modified output (script∗, v) = (script, v).
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


## 第 11 页

LI et al.: WOLVERINE: A SCALABLE AND TRANSACTION-CONSISTENT REDACTABLE PERMISSIONLESS BLOCKCHAIN
1663
If the output is spent, and (script∗, v) ̸= (script, v), the
modification will lead V (xC2) = 0 which contradicts the
redaction policy RP. Therefore, it must satisfy that G1 −O1 =
G2 −O2 and G1 ∼= G2 where O1, O2 be the unspent outputs
in G1, G2, respectively.
B. Security of the Committee Evolution Protocol
We prove the proposed DRB protocol is strong pseudo-
random in Appendix (cf. supplementary material). Now we
show that the committee generated by the committee election
protocol is the honest majority and no information on the
trapdoor key gsk of the chameleon hash is revealed during
the distributed key resharing protocol assuming the adversary
corrupts ρN parties with ρ < 1/2.
Theorem 5 (Good Majority): With unpredictable and unbi-
ased randomness, for every sufficiently large integer n ≥
n0 and 1 > u/(1 −2ρ): among the n identities published
on the chain, at most n/2 −1 are created by the adversary
with high probability.
Theorem 6 (Secrecy of DKRS): For every PPT adversary
A that corrupts up to t(e) < n(e)/2 parties in committee R(e)
and up to t(e+1) < n(e+1)/2 parties in committee R(e+1), A
learns nothing about gsk beyond the public parameters.
We postpone the complete proof to Appendix (cf. sup-
plementary material). Wolverine is scalable as the value of
n0 only depends on ρ and 1 given a failure parameter λ.
Specifically, when λ = 2−20, ρ = 0.4 and 1 = 50u, the size
of the committee is at least 2005.
VII. IMPLEMENTATION AND EVALUATION
In this section, we benchmark the NITCH, measure the com-
munication complexity of committee evolution protocol, and
evaluate the additional overhead of Wolverine compared with
Bitcoin. All our experiments were performed on a MacBook
Pro with an Intel Core i9-9880H CPU@2.30GHz and 32 GB
2667 MHz DDR4, running macOS 11.5 (Big Sur). Besides,
we set the fraction of corrupted computational power ρ = 0.4,
the runtime of committee election protocol 1 = 50u, the
failure parameter λ = 2−20, and the size of the committee
n = 2048.
A. Benchmarking of NITCH
We implemented the NITCH scheme in Construction 1
adopted the bls12-381 library [49] which provides a high-
speed BLS12-381 curve implementation. With the bls12-381
library, a bilinear pairing operation only takes about 0.6 ms.
To further speed up our NITCH implementation, we use
{ω, ω2, . . . , ωn} where ω is a primitive nth root of unity rather
than {1, 2, . . . , n} to compute parties’ shares of a polynomial,
i.e., the party Pi computes the share si, j = fi(ω j) rather than
fi( j) for j = 1, 2, . . . , n. We then adopt the fast polynomial
evaluation and interpolation algorithm from [29] to per-
form polynomial multipoint evaluation { fi(ω j)}i, j∈{1,2,...,n} in
DistKG protocol and compute Lagrange coefficients ℓI
i (0) =
Q
j∈I, j̸=i
0−ω j
ωi−ω j for i ∈I in Combine algorithm.
In Table I, we present a comparison of our NITCH and some
basic chameleon hash schemes, which are applied to other
TABLE I
COMPARISON OF CHAMELEON HASHES
TABLE II
BENCHMARKING OF NITCH
TABLE III
TOTAL COMMUNICATION OVERHEAD IN WORST-CASE
redactable blockchains, including DL-based CH [50], RSA-
based CH [43]. According to the NIST recommendation [51],
for 128-bit security, we chose 256 bits cyclic subgroup of
3072 bits group for DL-based CH, and 3072 bits modulus for
RSA-base CH. In Table II, we also benchmark our NITCH
to understand: (1) the time to generate a vote; (2) the time
to verify a vote; (3) the size of a vote and (4) the time
of combining votes to complete a modification. The results
demonstrate that our construction of NITCH is efficient.
B. Communication Complexity
In Table III, we list the on-chain and off-chain communica-
tion complexity of sub-protocols of the committee evolution
protocol. We measure on-chain communication complexity as
the total number of bits wrote on the chain, and measure
off-chain communication complexity as the total number of
bits transmitted in P2P channel. In the DRB protocol, each
player publishes an element of group G1 and a proof of
group Zp, i.e., 80 bytes of data on the chain. In the com-
mittee election protocol, each player publishes its identities
(PK, I P) and the corresponding PoW solution nonce, i.e.,
96 bytes of data on the chain. For the DKRS protocol, each
player publishes t elements of group G1, an element of group
G2 on the chain, and transmits n −1 elements of group
Zp to other players. In the worst case, the malicious players
complaints against all players and each player has to publish
additional t elements of Zp on the chain. Therefore, each
player publishes 48t + 96 + 32t bytes ≈80 KB of data on
the chain, and transmits 32(n −1) bytes ≈64 KB of data in
P2P channel.
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


## 第 12 页

1664
IEEE TRANSACTIONS ON INFORMATION FORENSICS AND SECURITY, VOL. 18, 2023
Fig. 4.
Additional overhead relative to Bitcoin.
Algorithm 1 validateRI
Input: C, RI
Output: {0, 1}
1: Parse RI := (txid, i, x∗)
2: tx ←C[txid]
3: script ←tx[i]
▷the i-th admissible field of tx
4: if script is OP_RETURN or coinbase then
5:
return 1
6: else if script is scriptSig then
7:
Retrieve the input (hash, n, scirpt) from tx
8:
Find the transaction tx1 with txid = hash
9:
Get tx′ by replacing script with x∗in tx
10:
return verifySig(tx′, tx1.vout[n].script PubKey)
11: else if script is script PubKey then
12:
Retrieve the output (nValue, script) from tx
13:
Find the transaction tx2 which redeems the output
14:
if tx2 == null then
15:
return 1
▷the output is not spent
16:
return verifySig(tx2, x∗)
17: return 0
C. Additional Overhead
We built Wolverine on top of Btcd [52] that is an implemen-
tation in Golang of bitcoin full nodes. In Bitcoin, a transaction
includes a version number, a locktime, a list of inputs of
the form (hash, n, scriptSig) and a list of outputs of the
form (nValue, script PubKey). Among these fields, only
scriptSig and script PubKey can be used to insert data.
Therefore, we define scriptSig and script PubKey as admis-
sible fields, and the other fields as stable fields. We instantiate
the validateRI algorithm of 5RB in Algorithm 1.
In order to evaluate the actual computation and storage bur-
den of the Wolverine compared with Bitcoin, we consider four
key indicators: (1) transaction generation time; (2) transaction
verification time; (3) block generation time; and (4) block
size. For each indicator, we tested the runtime of Bitcoin,
the runtime of Wolverine without vote(s) and the runtime of
Wolverine with vote(s). The transactions in the experiment are
of type P2PKH that is the common transaction templetas.
1) Transaction Generation (Verification) Time: We show a
comparison of the runtime of generating and verifying a trans-
action with different number of admissible fields in Fig. 4a
and Fig. 4b. The additional computation overhead increases
linearly in the number of admissible fields. For the most
common case where a transaction includes 2 inputs and
2 outputs, compared with Bitcoin, a node only needs extra
1.2 ms (1.33 ms) to generate (verify) a transaction without
votes and extra 1.7 ms (2.53 ms) to generate (verify) one
including a vote. The computational overhead is acceptable
in practice.
2) Block Generation Time: In Fig. 4c we show a com-
parison of the runtime of generating a block with different
number of transactions. We note that the additional time of
generating a block in Wolverine is almost constant compared
with Bitcoin when the transactions do not include votes.
If each transaction contains a vote, the block generation time
will increase significantly, but is still practical. Specifically,
when the number of transactions is 1500 and each transaction
contains a vote, the runtime of generating a block increases
by 2.46 s. Fortunately, the modifications in a blockchain are
not too frequent.
3) Block Size: In Fig. 4d, we show a comparison of the size
of a block with different number of transactions. Since we only
extend the block structure to accommodate a hash value of
the latest CVRL and keep the transaction structure the same,
the block size increase is constant (32 bytes) if no redaction
occurs. When the transactions contain votes, the additional
storage overhead increases linearly in the number of votes.
VIII. CONCLUSION
In this paper, we investigate the problem of redactable
permissionless blockchain. First, we introduce a novel primi-
tive NITCH with formal models, security definitions, and an
efficient pairing-based construction. Next, we present a generic
NITCH-to-DRB construction and prove the DRB protocol
meets strong pseudorandomness. We believe NITCH itself
can be useful for building other cryptography tools such
as non-interactive threshold sanitizable signatures. We then
construct Wolverine, a redactable permissionless blockchain
scheme with enhanced transaction consistency. The experi-
mental results show that Wolverine is both scalable and appli-
cable to Bitcoin without significantly reducing its performance.
REFERENCES
[1] S. Nakamoto, “Bitcoin: A peer-to-peer electronic cash system,” in
Decentralized Business Review. 2008, p. 21260.
[2] R. Matzutt, O. Hohlfeld, M. Henze, R. Rawiel, J. H. Ziegeldorf,
and K. Wehrle, “Poster: I don’t want that content! On the risks of
exploiting bitcoin’s blockchain as a content store,” in Proc. CCS, 2016,
pp. 1769–1771.
[3] R. Matzutt et al., “A quantitative analysis of the impact of arbitrary
blockchain content on bitcoin,” in Proc. FC, vol. 10957. Nieuwpoort,
Belgium: Springer, 2018, pp. 420–438.
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


## 第 13 页

LI et al.: WOLVERINE: A SCALABLE AND TRANSACTION-CONSISTENT REDACTABLE PERMISSIONLESS BLOCKCHAIN
1665
[4] G. Tziakouris, “Cryptocurrencies—A forensic challenge or opportunity
for law enforcement? An interpol perspective,” IEEE Secur. Privacy,
vol. 16, no. 4, pp. 92–94, Jul./Aug. 2018.
[5] (2016). General Data Protection Regulation. [Online]. Available:
https://gdpr-info.eu
[6] G. Ateniese, B. Magri, D. Venturi, and E. R. Andrade, “Redactable
blockchain–or–rewriting history in bitcoin and friends,” in Proc. IEEE
Eur. Symp. Secur. Privacy (EuroS&P), Apr. 2017, pp. 111–126.
[7] H. Krawczyk and T. Rabin, “Chameleon signatures,” in Proc. NDSS,
2000.
[8] M. Ben-Or, S. Goldwasser, and A. Wigderson, “Completeness theorems
for non-cryptographic fault-tolerant distributed computation (extended
abstract),” in Proc. 20th Annu. ACM Symp. Theory Comput., 1988,
pp. 1–10.
[9] G. Asharov and Y. Lindell, “A full proof of the BGW protocol for
perfectly-secure multiparty computation,” Electron. Colloq. Comput.
Complex., Tech. Rep. TR11-036, 2011.
[10] J. R. Douceur, “The sybil attack,” in Proc. Int. Workshop Peer–Peer
Syst. (Lecture Notes in Computer Science), vol. 2429. Cambridge, MA,
USA: Springer, 2002, pp. 251–260.
[11] D. Derler, K. Samelin, D. Slamanig, and C. Striecks, “Fine-grained and
controlled rewriting in blockchains: Chameleon-hashing gone attribute-
based,” in Proc. NDSS, 2019, pp. 1–51.
[12] S. Agrawal and M. Chase, “FAME: Fast attribute-based message encryp-
tion,” in Proc. CCS, 2017, pp. 665–682.
[13] Y. Tian, N. Li, Y. Li, P. Szalachowski, and J. Zhou, “Policy-based
chameleon hash for blockchain rewriting with black-box accountability,”
in Proc. Annu. Comput. Secur. Appl. Conf., Dec. 2020, pp. 813–828.
[14] S. Xu, J. Ning, J. Ma, G. Xu, J. Yuan, and R. H. Deng, “Revocable
policy-based chameleon hash,” in Proc. ESORICS (Lecture Notes in
Computer Science), vol. 12972. Darmstadt, Germany: Springer, 2021,
pp. 327–347.
[15] Z. Zhang, T. Li, Z. Wang, and J. Liu, “Redactable transactions in
consortium blockchain: Controlled by multi-authority CP-ABE,” in
Proc. ACISP, vol. 13083. Springer, 2021, pp. 408–429.
[16] J. Ma, S. Xu, J. Ning, X. Huang, and R. H. Deng, “Redactable
blockchain in decentralized setting,” IEEE Trans. Inf. Forensics Security,
vol. 17, pp. 1227–1242, 2022.
[17] Y. Jia, S.-F. Sun, Y. Zhang, Z. Liu, and D. Gu, “Redactable blockchain
supporting supervision and self-management,” in Proc. ACM Asia Conf.
Comput. Commun. Secur., May 2021, pp. 844–858.
[18] I. Puddu, A. Dmitrienko, and S. Capkun, “µchain: How to forget without
hard forks,” IACR Cryptol. ePrint Arch., Tech. Rep. 106, 2017.
[19] D. Deuber, B. Magri, and S. A. K. Thyagarajan, “Redactable blockchain
in the permissionless setting,” in Proc. IEEE Symp. Secur. Privacy (SP),
May 2019, pp. 124–138.
[20] S. A. K. Thyagarajan, A. Bhat, B. Magri, D. Tschudi, and A. Kate,
“Reparo: Publicly verifiable layer to repair blockchains,” in Proc. FC,
vol. 12675. Springer, 2021, pp. 37–56.
[21] R. Pass and E. Shi, “Fruitchains: A fair blockchain,” in Proc. PODC,
2017, pp. 315–324.
[22] H. Chernoff, “A measure of asymptotic efficiency for tests of a hypothe-
sis based on the sum of observations,” Ann. Math. Statist., vol. 23, no. 4,
pp. 493–507, Dec. 1952.
[23] I. Eyal and E. G. Sirer, “Majority is not enough: Bitcoin mining is
vulnerable,” in Proc. FC. Springer, 2014, pp. 436–454.
[24] J. A. Garay, A. Kiayias, and N. Leonardos, “The bitcoin backbone
protocol: Analysis and applications,” in Proc. EUROCRYPT (Lecture
Notes in Computer Science), vol. 9057. Sofia, Bulgaria: Springer, 2015,
pp. 281–310.
[25] M. Andrychowicz and S. Dziembowski, “PoW-based distributed cryp-
tography with no trusted setup,” in Proc. CRYPTO (Lecture Notes in
Computer Science), vol. 9216. Santa Barbara, CA, USA: Springer, 2015,
pp. 379–399.
[26] M. Zamani, M. Movahedi, and M. Raykova, “Rapidchain: Scaling
blockchain via full sharding,” in Proc. CCS, 2018, pp. 931–948.
[27] M. Bellare and P. Rogaway, “Robust computational secret sharing and
a unified account of classical secret-sharing goals,” in Proc. 14th ACM
Conf. Comput. Commun. Secur., Oct. 2007, pp. 172–184.
[28] A. Bishop, V. Pastro, R. Rajaraman, and D. Wichs, “Essentially optimal
robust secret sharing with maximal corruptions,” in Proc. EUROCRYPT.
Vienna, Austria: Springer, 2016, pp. 58–86.
[29] J. von zur Gathen and J. Gerhard, Fast Polynomial Evaluation and
Interpolation, 3rd ed. Cambridge, U.K.: Cambridge Univ. Press, 2013,
pp. 295–312.
[30] D. Galindo, J. Liu, M. Ordean, and J.-M. Wong, “Fully distributed
verifiable random functions and their application to decentralised ran-
dom beacons,” in Proc. IEEE Eur. Symp. Secur. Privacy (EuroS&P),
Sep. 2021, pp. 88–102.
[31] E. Syta et al., “Scalable bias-resistant distributed randomness,” in Proc.
SP, 2017, pp. 444–460.
[32] P. Schindler, A. Judmayer, N. Stifter, and E. Weippl, “HydRand: Efficient
continuous distributed randomness,” in Proc. SP, May 2020, pp. 73–89.
[33] R. Pass, L. Seeman, and A. Shelat, “Analysis of the blockchain protocol
in asynchronous networks,” in Proc. EUROCRYPT, 2017, pp. 643–673.
[34] A. Kiayias, A. Russell, B. David, and R. Oliynykov, “Ouroboros: A
provably secure proof-of-stake blockchain protocol,” in Proc. CRYPTO.
Santa Barbara, CA, USA: Springer, 2017, pp. 357–388.
[35] L. Luu, V. Narayanan, C. Zheng, K. Baweja, S. Gilbert, and P. Saxena,
“A secure sharding protocol for open blockchains,” in Proc. ACM
SIGSAC Conf. Comput. Commun. Secur., Oct. 2016, pp. 17–30.
[36] I. Abraham, S. Devadas, D. Dolev, K. Nayak, and L. Ren, “Efficient
synchronous byzantine consensus,” 2017, arXiv:1704.02397.
[37] M. Andrychowicz, S. Dziembowski, D. Malinowski, and L. Mazurek,
“Secure multiparty computations on bitcoin,” in Proc. IEEE Symp. Secur.
Privacy, May 2014, pp. 443–458.
[38] A. Kiayias, H. Zhou, and V. Zikas, “Fair and robust multi-party
computation using a global transaction ledger,” in Proc. EUROCRYPT.
Vienna, Austria: Springer, 2016, pp. 705–734.
[39] A. R. Choudhuri, M. Green, A. Jain, G. Kaptchuk, and I. Miers,
“Fairness in an unfair world: Fair multiparty computation from public
bulletin boards,” in Proc. CCS, 2017, pp. 719–728.
[40] S. K. D. Maram et al., “CHURP: Dynamic-committee proactive secret
sharing,” in Proc. CCS, 2019, pp. 2369–2386.
[41] N. Atzei, M. Bartoletti, S. Lande, and R. Zunino, “A formal model
of bitcoin transactions,” in Proc. FC. Nieuwpoort, Belgium: Springer,
2018, pp. 541–560.
[42] R. Gennaro, S. Jarecki, H. Krawczyk, and T. Rabin, “Secure distributed
key generation for discrete-log based cryptosystems,” in Proc. EURO-
CRYPT. Czech Republic: Springer, 1999, pp. 295–310.
[43] J. Camenisch, D. Derler, S. Krenn, H. C. Pöhls, K. Samelin, and
D. Slamanig, “Chameleon-hashes with ephemeral trapdoors: And appli-
cations to invisible sanitizable signatures,” in Proc. PKC. Amsterdam,
The Netherlands: Springer, 2017, pp. 152–182.
[44] S. D. Galbraith, K. G. Paterson, and N. P. Smart, “Pairings for cryptog-
raphers,” Discrete Appl. Math., vol. 156, no. 16, pp. 3113–3121, 2008.
[45] T. P. Pedersen, “Non-interactive and information-theoretic secure ver-
ifiable secret sharing,” in Proc. CRYPTO. Berlin, Germany: Springer,
1991, pp. 129–140.
[46] S. Micali and P. Rogaway, “Secure computation,” in Proc. Annu. Int.
Cryptol. Conf. Berlin, Germany: Springer, 1991, pp. 392–404.
[47] D. Chaum and T. P. Pedersen, “Wallet databases with observers,” in
Proc. CRYPTO. Berlin, Germany: Springer, 1992, pp. 89–105.
[48] P. Feldman, “A practical scheme for non-interactive verifiable secret
sharing,” in Proc. 28th Annu. Symp. Found. Comput. Sci. (SFCS),
Oct. 1987, pp. 427–437.
[49] (2019). High-Speed BLS12-381 Implementation in Go. [Online]. Avail-
able: https://github.com/kilic/bls12-381
[50] G. Ateniese and B. de Medeiros, “On the key exposure problem in
chameleon hashes,” in Security in Communication Networks. Berlin,
Germany: Springer, 2004, pp. 165–179.
[51] Nist Recommendations. Accessed: May 2020. [Online]. Available:
https://www.keylength.com/en/4/
[52] (2013). BTCD—An Alternative Full Node Bitcoin Implementation Writ-
ten in Go (Golang). [Online]. Available: https://github.com/btcsuite/btcd
Jianhao Li received the B.E. degree in information
security from the Huazhong University of Science
and Technology, Wuhan, China, in 2018. He is
currently pursuing the Ph.D. degree in information
security with the State Key Laboratory of Infor-
mation Security, Institute of Information Engineer-
ing, Chinese Academy of Sciences, Beijing, China.
His research interests include security of distributed
systems.
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


**提取到 1 张图片**

![page13_img1.png](Wolverine_images/page13_img1.png)

## 第 14 页

1666
IEEE TRANSACTIONS ON INFORMATION FORENSICS AND SECURITY, VOL. 18, 2023
Hui Ma received the B.E. degree in information
security from the Nanjing University of Aeronautics
and Astronautics, Nanjing, China, in 2012, and the
Ph.D. degree in information security from the Insti-
tute of Information Engineering, Chinese Academy
of Sciences, Beijing, China, in 2017. He is cur-
rently with the Institute of Information Engineering,
Chinese Academy of Sciences, as an Associate Pro-
fessor. He is working on applied cryptography and
cloud computing security.
Jiabei Wang received the B.E. degree in the Inter-
net of Things engineering from Sichuan University,
China, in 2016, and the Ph.D. degree in cyber secu-
rity from the State Key Laboratory of Information
Security, Institute of Information Engineering, Chi-
nese Academy of Sciences, Beijing, China, in 2022.
She is currently with the School of Cyber Science
and Engineering, Nanjing University of Science
and Technology, as a Post-Doctoral Researcher. Her
research interests include applied cryptography and
information security.
Zishuai Song received the B.E. degree in informa-
tion security from Xidian University, China, in 2017.
He is currently pursuing the Ph.D. degree in infor-
mation security with the State Key Laboratory of
Information Security, Institute of Information Engi-
neering, Chinese Academy of Sciences, Beijing,
China. His research interests include security mech-
anisms in cloud computing.
Wenhan Xu received the B.E. degree in information
security from the Huazhong University of Science
and Technology, Wuhan, China, in 2018. He is
currently pursuing the Ph.D. degree in information
security with the State Key Laboratory of Informa-
tion Security, Institute of Information Engineering,
Chinese Academy of Sciences, Beijing, China. His
research interests include security mechanisms in
cloud computing.
Rui Zhang received the B.E. degree from Tsinghua
University and the M.S. and Ph.D. degrees from
The University of Tokyo. He was a JSPS Research
Fellow before he joined AIST, Japan, as a Research
Scientist. He is currently with the Institute of Infor-
mation Engineering (IIE), Chinese Academy of Sci-
ences, as a Research Professor. His research interests
include applied cryptography, network security, and
information theory.
Authorized licensed use limited to: Nanjing University. Downloaded on May 16,2025 at 09:45:09 UTC from IEEE Xplore.  Restrictions apply. 


**提取到 5 张图片**

![page14_img1.png](Wolverine_images/page14_img1.png)

![page14_img2.png](Wolverine_images/page14_img2.png)

![page14_img3.png](Wolverine_images/page14_img3.png)

![page14_img4.png](Wolverine_images/page14_img4.png)

![page14_img5.png](Wolverine_images/page14_img5.png)

