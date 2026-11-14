// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.0;
contract PermissionManagerV3 {
    uint256 private constant BN256_P = 21888242871839275222246405745257275088696311157297823662689037894645226208583;
    uint256 private constant BN256_R = 21888242871839275222246405745257275088548364400416034343698204186575808495617;
    uint256 private constant SECP256K1_N_DIV_2 = 57896044618658097711785492504343953926418782139537452191302581570759080747168;
    bytes private constant COMMITTEE_DST = "committee-sig/mvp/v1";
    uint16 public constant REDACT_PROTOCOL_VERSION = 1;
    bytes32 private constant REQ_DOMAIN = keccak256("T_GOVERN_REDACT_REQ_V1");
    bytes32 private constant AUTH_DOMAIN = keccak256("T_GOVERN_REDACT_AUTH_V1");
    bytes32 private constant REJECT_DOMAIN = keccak256("T_GOVERN_REDACT_REJECT_V1");
    bytes32 private constant ERASE_DOMAIN = keccak256("T_GOVERN_TEE_ERASE_V1");
    bytes32 private constant REVOKE_DOMAIN = keccak256("T_GOVERN_TEE_REVOKE_V1");
    bytes32 private constant VRF_DOMAIN = keccak256("T_GOVERN_TEE_VRF_V1");
    bytes32 private constant PKCH_DOMAIN = keccak256("T_GOVERN_PKCH_SET_V1");
    bytes32 private constant ERASE_TIMEOUT_REASON = keccak256("T_GOVERN_TEE_ERASE_TIMEOUT");
    uint256 public constant FINALITY_BLOCK_DELAY = 6;
    uint256 public constant ROTATION_PREPARE_BLOCKS = 20;
    uint256 private constant DEFAULT_ERASE_PROOF_GRACE_BLOCKS = 50;
    uint256 private constant MAX_ERASE_TIMEOUT_SLASHES_PER_CALL = 8;

    uint256 public MIN_DEPOSIT;
    uint256 public nT;
    uint256 public tT;
    uint256 public epochLength;
    uint256 public steadyEpochLength;
    uint256 public currentEpoch = 0;
    uint256 public genesisTimestamp;
    uint256 public lastRotationTimestamp;
    uint256 public lastRotationBlock;
    uint256 public committeePubKeyVersion = 0;
    bytes public pkCH;
    uint256 public pkCHVersion = 0;
    uint256 public eraseProofGraceBlocks;

    struct TEE {
        bytes32 HardwareId;
        bytes32 MR_ENCLAVE;
        bytes32 sigPubKey;
        bytes32 vrfPubKey;
        bytes32[2] enclavePK;
        address owner;
        uint256 deposit;
        uint256 indexInActiveList;
        TEEStatus status;
    }

    struct PendingTEE {
        uint256 deposit;
        uint256 nonce;
        bytes32 challengeHash;
    }

    enum TEEStatus {
        None,
        Pending,
        Candidate,
        Active,
        Exited,
        Revoked
    }

    enum RedactStatus {
        None,
        Authorized,
        Executed,
        Rejected,
        Expired
    }

    struct ReqFields {
        uint16 version;
        uint64 epoch;
        bytes32 reqID;
        // [txID, mHash, mPrimeHash, posHash, metaHash, activeSetHash, pkCHHash]
        bytes32[7] bodyHashes;
    }

    struct ShareProof {
        bytes32 sigPubKey;
        bytes sI;
        bytes sigma;
    }

    struct RedactRequestRecord {
        uint256 epoch;
        bytes32 dReq;
        bytes32 activeSetHash;
        RedactStatus status;
        uint256 expiresAt;
    }

    struct TEERegistrationEvidence {
        bytes32 sigPubKey;
        bytes32 vrfPubKey;
        bytes32[2] enclavePK;
        bytes32 mrSigner;
        bytes32 quoteHash;
        bytes32 registerCertHash;
        bytes32 challengeHash;
    }

    struct VRFSubmission {
        bool exists;
        bytes32 vrfResult;
        bytes32 proofHash;
        uint256 submittedAtBlock;
        uint256 submittedAt;
    }

    struct TEEExitRecord {
        bool pending;
        address owner;
        uint256 exitEpoch;
        uint256 eraseProofDeadlineBlock;
        bytes32 eraseProofHash;
        uint256 finalizedAt;
    }

    address public owner;
    TEE[] private candidateTEEs;
    mapping(address => PendingTEE) public pendingTEEOfOwner;
    mapping(address => bytes32) public sigPubKeyOfOwner;
    mapping(address => uint256) public nextPendingNonceOfOwner;
    mapping(bytes32 => uint256) private hardwareIdRegistrationCount;
    mapping(bytes32 => bool) public isSigPubKeyRegistered;
    mapping(bytes32 => uint256) public indexOfSigPubKey;
    uint256[4] public committeePubKey;
    uint256[] public activeTEEs;
    mapping(uint256 => bytes32) public activeSetHashByEpoch;
    mapping(uint256 => bytes32) public seedByEpoch;
    mapping(uint256 => uint256) public finalityBlockByEpoch;
    mapping(uint256 => bytes32) public finalityBlockHashByEpoch;
    mapping(uint256 => bool) public rotationPreparationRequested;
    mapping(uint256 => mapping(bytes32 => VRFSubmission)) private vrfSubmissions;
    mapping(bytes32 => TEEExitRecord) private teeExitRecords;
    mapping(bytes32 => RedactRequestRecord) private redactRequests;
    mapping(bytes32 => bool) public trustedMREnclave;
    uint256 public trustedMREnclaveCount;
    bool private initialActiveSetBootstrapped;

    event TEERegistered(bytes32 MR_ENCLAVE, bytes32 indexed HardwareId, bytes32 indexed sigPubKey, bytes32 enclavePKHash);
    event TEERegistrationEvidenceRecorded(
        bytes32 indexed HardwareId,
        bytes32 sigPubKey,
        bytes32 vrfPubKey,
        bytes32 enclavePKHash,
        bytes32 mrSigner,
        bytes32 quoteHash,
        bytes32 registerCertHash,
        bytes32 challengeHash,
        uint256 registeredAt
    );
    event TEEStakePending(address indexed owner, uint256 indexed pendingNonce, uint256 deposit, bytes32 challengeHash);
    event PendingTEECancelled(address indexed owner, uint256 indexed pendingNonce, uint256 deposit);
    event TEEExitPending(bytes32 indexed HardwareId, address indexed owner, uint256 indexed fromEpoch, uint256 nextEpoch, uint256 exitedAt);
    event TEEEraseProofDeadlineSet(bytes32 indexed HardwareId, address indexed owner, uint256 indexed exitEpoch, uint256 deadlineBlock);
    event TEEExited(bytes32 indexed HardwareId, address indexed owner, uint256 indexed epoch, bytes32 eraseProofHash, uint256 exitedAt, uint256 refundedDeposit);
    event TEERevoked(bytes32 indexed HardwareId, address indexed owner, bytes32 reasonHash, uint256 revokedAt, uint256 slashedAmount);
    event CommitteePubKeyRegistered(address indexed caller, uint256[4] pubKey);
    event TrustedMREnclaveSet(bytes32 indexed mrEnclave, bool trusted);
    event PkCHSet(uint256 indexed version, bytes32 indexed pkCHHash);
    event ActiveSetCheckpointed(uint256 indexed epoch, bytes32 indexed activeSetHash, bytes32[] hardwareIds);
    event ActiveSetRotated(
        uint256 indexed epoch,
        bytes32 indexed activeSetHash,
        uint256 rotatedAt,
        uint256 finalityBlockNumber,
        bytes32 finalityBlockHash,
        bytes32 seed,
        bytes32[] hardwareIds
    );
    event TEERotationPreparationRequested(
        uint256 indexed currentEpoch,
        uint256 indexed nextEpoch,
        uint256 deadlineBlock,
        uint256 candidateCount,
        uint256 finalityBlockNumber,
        bytes32 finalityBlockHash,
        bytes32 seed
    );
    event TEEVRFSubmitted(
        uint256 indexed epoch,
        bytes32 indexed HardwareId,
        bytes32 vrfPubKey,
        bytes32 vrfResult,
        bytes32 proofHash,
        uint256 submittedAtBlock,
        uint256 submittedAt
    );
    event RedactRequestPayloadRecorded(
        bytes32 indexed reqID,
        bytes32 txID,
        bytes32 mHash,
        bytes32 mPrimeHash,
        bytes32 posHash,
        bytes32 metaHash,
        bytes32 activeSetHash,
        bytes32 pkCHHash
    );
    event RedactAuthorized(
        bytes32 indexed reqID,
        uint256 indexed epoch,
        bytes32 dReq,
        bytes32 dPolicy,
        bytes32 activeSetHash,
        bytes32 pkCHHash,
        uint256 committeePubKeyVersion,
        uint256 expiresAt,
        uint256[2] signature
    );
    event RedactExecuted(bytes32 indexed reqID, bytes32 rPrimeHash, bytes32 proofHash, bytes32 resultHash, bytes32 piReqAHash, bytes piReqA);
    event RedactRejected(bytes32 indexed reqID, bytes32 dReq, bytes32 dPolicy, bytes32 reasonHash, uint256[2] signature);
    event RedactExpired(bytes32 indexed reqID, uint256 expiredAt);
    event ActiveSetRotationDeferred(uint256 indexed epoch, uint256 submittedCount, uint256 requiredCount);

    modifier onlyOwner() {
        require(msg.sender == owner, "Not contract owner");
        _;
    }

    modifier rotateIfNeeded() {
        _requestRotationPreparationIfNeeded();
        _rotateActiveSetIfNeeded();
        _slashExpiredEraseProofsIfNeeded();
        _;
    }

    modifier onlyCurrentEpochActiveTEE() {
        require(_isCurrentEpochActiveTEEOwner(msg.sender), "Caller is not current active TEE");
        _;
    }

    constructor(uint256 _roundBlocks, uint256 _roundTimeout, uint256 _minDeposit, uint256 _maxValidators, uint256 _teeNum) {
        owner = msg.sender;
        MIN_DEPOSIT = _minDeposit;
        nT = _teeNum;
        tT = _maxValidators == 0 ? 1 : _maxValidators;
        epochLength = _roundBlocks;
        steadyEpochLength = _roundTimeout == 0 ? _roundBlocks : _roundTimeout;
        genesisTimestamp = block.timestamp;
        lastRotationTimestamp = block.timestamp;
        lastRotationBlock = block.number;
        eraseProofGraceBlocks = DEFAULT_ERASE_PROOF_GRACE_BLOCKS;
        _checkpointActiveSet(currentEpoch);
    }

    function registerCommitteePubKey(uint256[4] calldata _committeePubKey) external rotateIfNeeded returns (bool) {
        require(msg.sender == owner, "Only owner can register");
        require(_isCommitteePubKeyEmpty(), "Committee pubkey already registered");
        require(
            _committeePubKey[0] != 0 ||
            _committeePubKey[1] != 0 ||
            _committeePubKey[2] != 0 ||
            _committeePubKey[3] != 0,
            "Invalid committee pubkey"
        );

        committeePubKey = _committeePubKey;
        committeePubKeyVersion++;
        emit CommitteePubKeyRegistered(msg.sender, _committeePubKey);
        return true;
    }

    function setTrustedMREnclave(bytes32 mrEnclave, bool trusted) external onlyOwner rotateIfNeeded returns (bool) {
        require(mrEnclave != bytes32(0), "MR_ENCLAVE required");
        if (trustedMREnclave[mrEnclave] == trusted) {
            return true;
        }
        trustedMREnclave[mrEnclave] = trusted;
        if (trusted) {
            trustedMREnclaveCount++;
        } else {
            trustedMREnclaveCount--;
        }
        emit TrustedMREnclaveSet(mrEnclave, trusted);
        return true;
    }

    function _isCommitteePubKeyEmpty() internal view returns (bool) {
        return committeePubKey[0] == 0 && committeePubKey[1] == 0 && committeePubKey[2] == 0 && committeePubKey[3] == 0;
    }

    function verifyAggregatedSignature(
        bytes32 messageHash,
        uint256[2] calldata signature
    ) internal view returns (bool) {
        require(!_isCommitteePubKeyEmpty(), "Committee pubkey not set");
        require(signature[0] != 0 || signature[1] != 0, "Invalid signature");

        uint256[2] memory hm = _hashToG1(messageHash);
        uint256[2] memory negHm = [hm[0], hm[1] == 0 ? 0 : BN256_P - hm[1]];
        return _pairingCheck(signature, _g2Generator(), negHm, committeePubKey);
    }

    function _hashToG1(bytes32 messageHash) internal view returns (uint256[2] memory) {
        uint256 s = uint256(sha256(abi.encodePacked(messageHash, COMMITTEE_DST))) % BN256_R;
        if (s == 0) {
            s = 1;
        }
        return _bn256ScalarMul([uint256(1), uint256(2)], s);
    }

    function _g2Generator() internal pure returns (uint256[4] memory) {
        return [
            uint256(11559732032986387107991004021392285783925812861821192530917403151452391805634),
            uint256(10857046999023057135944570762232829481370756359578518086990519993285655852781),
            uint256(4082367875863433681332203403145435568316851327593401208105741076214120093531),
            uint256(8495653923123431417604973247489272438418190587263600148770280649306958101930)
        ];
    }

    function _bn256ScalarMul(uint256[2] memory p, uint256 s) internal view returns (uint256[2] memory r) {
        bytes memory input = abi.encodePacked(p[0], p[1], s);
        bool success;
        bytes memory out = new bytes(64);
        assembly {
            success := staticcall(gas(), 0x07, add(input, 0x20), 96, add(out, 0x20), 64)
        }
        require(success, "bn256 scalar mul failed");
        assembly {
            mstore(r, mload(add(out, 0x20)))
            mstore(add(r, 0x20), mload(add(out, 0x40)))
        }
    }

    function _pairingCheck(
        uint256[2] memory a1,
        uint256[4] memory a2,
        uint256[2] memory b1,
        uint256[4] memory b2
    ) internal view returns (bool) {
        bytes memory input = abi.encodePacked(
            a1[0], a1[1], a2[0], a2[1], a2[2], a2[3],
            b1[0], b1[1], b2[0], b2[1], b2[2], b2[3]
        );
        uint256[1] memory out;
        bool success;
        assembly {
            success := staticcall(gas(), 0x08, add(input, 0x20), 384, out, 0x20)
        }
        return success && out[0] == 1;
    }

    function isHardwareIdRegistered(bytes32 hardwareId) public view returns (bool) {
        return hardwareIdRegistrationCount[hardwareId] > 0;
    }

    function _hasRegisteredTEE(address _owner) internal view returns (bool, bytes32) {
        bytes32 registeredSigPubKey = sigPubKeyOfOwner[_owner];
        if (registeredSigPubKey == bytes32(0)) {
            return (false, bytes32(0));
        }
        if (!isSigPubKeyRegistered[registeredSigPubKey]) {
            return (false, bytes32(0));
        }
        return (true, registeredSigPubKey);
    }

    function _getTEEStatus(address _owner) internal view returns (TEEStatus, bytes32) {
        if (_hasPendingTEE(_owner)) {
            return (TEEStatus.Pending, bytes32(0));
        }

        (bool registered, bytes32 registeredSigPubKey) = _hasRegisteredTEE(_owner);
        if (!registered) {
            return (TEEStatus.None, bytes32(0));
        }

        TEE memory tee = candidateTEEs[indexOfSigPubKey[registeredSigPubKey]];
        return (tee.status, registeredSigPubKey);
    }

    function _hasPendingTEE(address _owner) internal view returns (bool) {
        return pendingTEEOfOwner[_owner].nonce != 0;
    }

    function _isNonZeroEnclavePK(bytes32[2] memory enclavePK) internal pure returns (bool) {
        return enclavePK[0] != bytes32(0) || enclavePK[1] != bytes32(0);
    }

    function _refundStake(address recipient, uint256 amount) internal {
        if (amount == 0) {
            return;
        }
        (bool success, ) = payable(recipient).call{value: amount}("");
        require(success, "Refund failed");
    }

    function _recordEraseProofFinalized(
        bytes32 hardwareId,
        address teeOwner,
        uint256 epoch,
        uint256 eraseProofDeadlineBlock,
        bytes32 eraseProofHash
    ) internal {
        teeExitRecords[hardwareId] = TEEExitRecord({
            pending: false,
            owner: teeOwner,
            exitEpoch: epoch,
            eraseProofDeadlineBlock: eraseProofDeadlineBlock,
            eraseProofHash: eraseProofHash,
            finalizedAt: block.timestamp
        });
    }

    function _validatePendingEraseProof(bytes32 hardwareId, address teeOwner, uint256 epoch) internal view {
        TEEExitRecord storage exitRecord = teeExitRecords[hardwareId];
        if (!exitRecord.pending) {
            return;
        }
        require(exitRecord.owner == teeOwner, "Exit owner mismatch");
        require(exitRecord.exitEpoch == epoch, "Exit epoch mismatch");
    }

    function _finalizeEraseProof(
        bytes32 hardwareId,
        uint256 index,
        uint256 epoch,
        bytes32 eraseProofHash
    ) internal {
        bool activeSetChanged = _removeFromActiveSet(hardwareId);
        TEE storage tee = candidateTEEs[index];
        uint256 depositAmount = tee.deposit;
        address teeOwner = tee.owner;
        uint256 eraseProofDeadlineBlock = teeExitRecords[hardwareId].eraseProofDeadlineBlock;

        tee.deposit = 0;
        tee.status = TEEStatus.Exited;
        _recordEraseProofFinalized(hardwareId, teeOwner, epoch, eraseProofDeadlineBlock, eraseProofHash);
        if (activeSetChanged) {
            _checkpointActiveSet(currentEpoch);
        }
        _deleteTEERegistration(index);
        _refundStake(teeOwner, depositAmount);
        emit TEEExited(hardwareId, teeOwner, epoch, eraseProofHash, block.timestamp, depositAmount);
    }

    function stakeTEE() external payable rotateIfNeeded returns (uint256 pendingNonce) {
        require(!_hasPendingTEE(msg.sender), "Pending TEE already exists");
        (bool registered, ) = _hasRegisteredTEE(msg.sender);
        require(!registered, "TEE already registered for owner");
        require(msg.value >= MIN_DEPOSIT, "Deposit required");

        pendingNonce = nextPendingNonceOfOwner[msg.sender] + 1;
        nextPendingNonceOfOwner[msg.sender] = pendingNonce;
        bytes32 parentHash = block.number == 0 ? bytes32(0) : blockhash(block.number - 1);
        bytes32 challengeHash = keccak256(abi.encodePacked(address(this), msg.sender, pendingNonce, block.chainid, block.number, block.timestamp, parentHash));

        pendingTEEOfOwner[msg.sender] = PendingTEE({
            deposit: msg.value,
            nonce: pendingNonce,
            challengeHash: challengeHash
        });

        emit TEEStakePending(msg.sender, pendingNonce, msg.value, challengeHash);
        return pendingNonce;
    }

    function cancelPendingTEE() external rotateIfNeeded returns (bool) {
        require(_hasPendingTEE(msg.sender), "Pending TEE not found");

        PendingTEE memory pending = pendingTEEOfOwner[msg.sender];
        delete pendingTEEOfOwner[msg.sender];

        _refundStake(msg.sender, pending.deposit);
        emit PendingTEECancelled(msg.sender, pending.nonce, pending.deposit);
        return true;
    }

    function registerTEE(
        address _owner,
        uint256 _pendingNonce,
        bytes32 _MR_ENCLAVE,
        bytes32 _HardwareId,
        TEERegistrationEvidence calldata evidence,
        uint256[2] calldata _signature
    ) external rotateIfNeeded returns (bool) {
        require(_owner != address(0), "Invalid owner");
        require(_MR_ENCLAVE != bytes32(0), "MR_ENCLAVE required");
        require(_HardwareId != bytes32(0), "Hardware ID required");
        require(evidence.sigPubKey != bytes32(0), "Signature public key required");
        require(evidence.vrfPubKey != bytes32(0), "VRF public key required");
        require(_isNonZeroEnclavePK(evidence.enclavePK), "Enclave public key required");
        require(evidence.quoteHash != bytes32(0), "Quote hash required");
        require(evidence.registerCertHash != bytes32(0), "Register cert hash required");
        require(evidence.challengeHash != bytes32(0), "Challenge hash required");
        require(uint256(evidence.sigPubKey) <= type(uint160).max, "Signature public key must be address");
        require(uint256(evidence.vrfPubKey) <= type(uint160).max, "VRF public key must be address");
        require(_hasPendingTEE(_owner), "Pending TEE not found");
        require(trustedMREnclaveCount == 0 || trustedMREnclave[_MR_ENCLAVE], "MR_ENCLAVE not trusted");

        PendingTEE memory pending = pendingTEEOfOwner[_owner];
        require(pending.nonce == _pendingNonce, "Pending nonce mismatch");
        require(pending.challengeHash == evidence.challengeHash, "Challenge mismatch");
        (bool registered, ) = _hasRegisteredTEE(_owner);
        require(!registered, "TEE already registered for owner");

        bytes32 messageHash = keccak256(abi.encode(
            _owner,
            _pendingNonce,
            _MR_ENCLAVE,
            _HardwareId,
            evidence.sigPubKey,
            evidence.vrfPubKey,
            evidence.enclavePK,
            evidence.mrSigner,
            evidence.quoteHash,
            evidence.registerCertHash,
            evidence.challengeHash
        ));
        require(verifyAggregatedSignature(messageHash, _signature), "Invalid signature");
        require(!isSigPubKeyRegistered[evidence.sigPubKey], "Signature public key already registered");

        candidateTEEs.push(TEE({
            HardwareId: _HardwareId,
            MR_ENCLAVE: _MR_ENCLAVE,
            sigPubKey: evidence.sigPubKey,
            vrfPubKey: evidence.vrfPubKey,
            enclavePK: evidence.enclavePK,
            owner: _owner,
            deposit: pending.deposit,
            indexInActiveList: 0,
            status: TEEStatus.Candidate
        }));
        isSigPubKeyRegistered[evidence.sigPubKey] = true;
        hardwareIdRegistrationCount[_HardwareId]++;
        indexOfSigPubKey[evidence.sigPubKey] = candidateTEEs.length - 1;
        sigPubKeyOfOwner[_owner] = evidence.sigPubKey;

        delete pendingTEEOfOwner[_owner];

        bytes32 enclavePKHash = keccak256(abi.encodePacked(evidence.enclavePK[0], evidence.enclavePK[1]));
        emit TEERegistered(_MR_ENCLAVE, _HardwareId, evidence.sigPubKey, enclavePKHash);
        emit TEERegistrationEvidenceRecorded(
            _HardwareId,
            evidence.sigPubKey,
            evidence.vrfPubKey,
            enclavePKHash,
            evidence.mrSigner,
            evidence.quoteHash,
            evidence.registerCertHash,
            evidence.challengeHash,
            block.timestamp
        );
        _bootstrapInitialActiveSetIfReady();
        return true;
    }

    function _isTEEOwner(bytes32 sigPubKey) internal view returns (bool) {
        uint256 index = indexOfSigPubKey[sigPubKey];
        return candidateTEEs[index].owner == msg.sender;
    }

    function _removeFromActiveSet(bytes32 sigPubKey) internal returns (bool) {
        uint256 index = indexOfSigPubKey[sigPubKey];
        if (candidateTEEs[index].status != TEEStatus.Active) {
            return false;
        }

        uint256 activeIndex = candidateTEEs[index].indexInActiveList;
        uint256 lastActiveIndex = activeTEEs.length - 1;
        if (activeIndex != lastActiveIndex) {
            uint256 movedActiveTeeIndex = activeTEEs[lastActiveIndex];
            activeTEEs[activeIndex] = movedActiveTeeIndex;
            candidateTEEs[movedActiveTeeIndex].indexInActiveList = activeIndex;
        }
        activeTEEs.pop();
        candidateTEEs[index].indexInActiveList = 0;
        return true;
    }

    function _deleteTEERegistration(uint256 index) internal {
        TEE memory tee = candidateTEEs[index];
        delete isSigPubKeyRegistered[tee.sigPubKey];
        if (hardwareIdRegistrationCount[tee.HardwareId] > 0) {
            hardwareIdRegistrationCount[tee.HardwareId]--;
        }
        delete indexOfSigPubKey[tee.sigPubKey];
        if (sigPubKeyOfOwner[tee.owner] == tee.sigPubKey) {
            delete sigPubKeyOfOwner[tee.owner];
        }

        uint256 lastIndex = candidateTEEs.length - 1;
        if (index != lastIndex) {
            TEE memory moved = candidateTEEs[lastIndex];
            candidateTEEs[index] = moved;
            indexOfSigPubKey[moved.sigPubKey] = index;
            if (moved.status == TEEStatus.Active) {
                activeTEEs[moved.indexInActiveList] = index;
            }
        }
        candidateTEEs.pop();
    }

    function _verifyTEESignature(bytes32 messageHash, bytes32 sigPubKey, bytes memory signature) internal pure returns (bool) {
        require(signature.length == 65, "Invalid TEE signature length");

        bytes32 r;
        bytes32 s;
        uint8 v;
        assembly {
            r := mload(add(signature, 32))
            s := mload(add(signature, 64))
            v := byte(0, mload(add(signature, 96)))
        }
        if (v < 27) {
            v += 27;
        }
        require(v == 27 || v == 28, "Invalid TEE recovery id");
        require(uint256(s) <= SECP256K1_N_DIV_2, "Invalid TEE signature");

        address recovered = ecrecover(messageHash, v, r, s);
        address expected = address(uint160(uint256(sigPubKey)));
        return recovered != address(0) && recovered == expected;
    }

    function submitEraseProof(
        bytes32 sigPubKey,
        uint256 epoch,
        bytes32 eraseProofHash,
        bytes calldata signature
    ) external rotateIfNeeded returns (bool) {
        require(isSigPubKeyRegistered[sigPubKey], "Signature public key not registered");
        require(_isTEEOwner(sigPubKey), "Only TEE owner can exit");
        require(epoch <= currentEpoch, "Epoch in future");
        require(eraseProofHash != bytes32(0), "Erase proof required");

        uint256 index = indexOfSigPubKey[sigPubKey];
        TEE storage tee = candidateTEEs[index];
        require(
            tee.status == TEEStatus.Candidate || tee.status == TEEStatus.Active || tee.status == TEEStatus.Exited,
            "TEE cannot submit erase proof"
        );
        require(tee.deposit > 0, "TEE already finalized");
        _validatePendingEraseProof(sigPubKey, msg.sender, epoch);
        require(
            _verifyTEESignature(computeEraseProofDigest(sigPubKey, msg.sender, epoch, eraseProofHash), tee.sigPubKey, signature),
            "Invalid erase proof signature"
        );
        _finalizeEraseProof(sigPubKey, index, epoch, eraseProofHash);

        return true;
    }

    function revokeTEE(bytes32 sigPubKey, bytes32 reasonHash, uint256 slashAmount, uint256[2] calldata _signature) external returns (bool) {
        _requestRotationPreparationIfNeeded();
        _rotateActiveSetIfNeeded();

        require(isSigPubKeyRegistered[sigPubKey], "Signature public key not registered");
        require(reasonHash != bytes32(0), "Reason hash required");
        bytes32 messageHash = computeRevocationDigest(sigPubKey, reasonHash, slashAmount, committeePubKeyVersion);
        require(verifyAggregatedSignature(messageHash, _signature), "Invalid signature");

        uint256 index = indexOfSigPubKey[sigPubKey];
        TEE storage tee = candidateTEEs[index];
        require(
            tee.status == TEEStatus.Candidate || tee.status == TEEStatus.Active || tee.status == TEEStatus.Exited,
            "TEE cannot be revoked"
        );
        if (tee.status == TEEStatus.Exited) {
            require(_isEraseProofOverdue(sigPubKey), "Erase proof deadline not passed");
        }
        require(slashAmount <= tee.deposit, "Slash exceeds deposit");

        _removeFromActiveSet(sigPubKey);
        tee.deposit = tee.deposit - slashAmount;
        tee.status = TEEStatus.Revoked;
        if (teeExitRecords[sigPubKey].pending) {
            teeExitRecords[sigPubKey].pending = false;
        }
        _checkpointActiveSet(currentEpoch);
        emit TEERevoked(sigPubKey, tee.owner, reasonHash, block.timestamp, slashAmount);
        _slashExpiredEraseProofsIfNeeded();

        return true;
    }

    function rotationDeadlineBlock() public view returns (uint256) {
        return lastRotationBlock + _currentEpochLength();
    }

    function prepareRotation() external returns (bool) {
        return _requestRotationPreparationIfNeeded();
    }

    function isRotationPreparationPhase() public view returns (bool) {
        uint256 currentLength = _currentEpochLength();
        if (currentLength == 0) {
            return false;
        }
        uint256 deadlineBlock = rotationDeadlineBlock();
        return block.number < deadlineBlock && block.number + ROTATION_PREPARE_BLOCKS >= deadlineBlock;
    }

    function submitTEESelectionVRF(
        uint256 epoch,
        bytes32 sigPubKey,
        bytes32 vrfResult,
        bytes calldata vrfProof
    ) external returns (bool) {
        _requestRotationPreparationIfNeeded();

        require(epoch == currentEpoch + 1, "Epoch mismatch");
        require(seedByEpoch[epoch] != bytes32(0), "Rotation seed not ready");
        require(
            isRotationPreparationPhase() || (_isRotationDue() && _vrfSubmissionCount(epoch) < nT),
            "Not accepting rotation VRF"
        );
        require(isSigPubKeyRegistered[sigPubKey], "Signature public key not registered");
        require(_isTEEOwner(sigPubKey), "Only TEE owner can submit VRF");
        require(vrfResult != bytes32(0), "VRF result required");
        require(vrfProof.length > 0, "VRF proof required");
        require(!vrfSubmissions[epoch][sigPubKey].exists, "VRF already submitted");

        uint256 index = indexOfSigPubKey[sigPubKey];
        TEE storage tee = candidateTEEs[index];
        require(tee.status == TEEStatus.Candidate || tee.status == TEEStatus.Active, "TEE not eligible");
        require(tee.vrfPubKey != bytes32(0), "VRF public key missing");

        bool recorded = _recordTEESelectionVRF(epoch, sigPubKey, tee.vrfPubKey, vrfResult, vrfProof);
        _rotateActiveSetIfNeeded();
        _slashExpiredEraseProofsIfNeeded();
        return recorded;
    }

    function _recordTEESelectionVRF(
        uint256 epoch,
        bytes32 sigPubKey,
        bytes32 vrfPubKey,
        bytes32 vrfResult,
        bytes calldata vrfProof
    ) internal returns (bool) {
        bytes32 seed = seedByEpoch[epoch];
        bytes32 verifiedVRFResult = _verifyAndDeriveVRFResult(
            epoch,
            sigPubKey,
            vrfPubKey,
            seed,
            vrfProof
        );
        require(vrfResult == verifiedVRFResult, "VRF result mismatch");

        bytes32 proofHash = keccak256(abi.encode(
            VRF_DOMAIN,
            block.chainid,
            address(this),
            epoch,
            sigPubKey,
            vrfPubKey,
            seed,
            vrfResult,
            keccak256(vrfProof)
        ));

        vrfSubmissions[epoch][sigPubKey] = VRFSubmission({
            exists: true,
            vrfResult: vrfResult,
            proofHash: proofHash,
            submittedAtBlock: block.number,
            submittedAt: block.timestamp
        });
        emit TEEVRFSubmitted(epoch, sigPubKey, vrfPubKey, vrfResult, proofHash, block.number, block.timestamp);
        return true;
    }

    function getTEESelectionVRF(uint256 epoch, bytes32 sigPubKey) external view returns (
        bool exists,
        bytes32 vrfResult,
        bytes32 proofHash,
        uint256 submittedAtBlock,
        uint256 submittedAt
    ) {
        VRFSubmission storage submission = vrfSubmissions[epoch][sigPubKey];
        return (
            submission.exists,
            submission.vrfResult,
            submission.proofHash,
            submission.submittedAtBlock,
            submission.submittedAt
        );
    }

    function getTEEExitInfo(bytes32 sigPubKey) external view returns (
        bool pending,
        address teeOwner,
        uint256 exitEpoch,
        uint256 eraseProofDeadlineBlock,
        bytes32 eraseProofHash,
        uint256 finalizedAt
    ) {
        TEEExitRecord storage record = teeExitRecords[sigPubKey];
        return (
            record.pending,
            record.owner,
            record.exitEpoch,
            record.eraseProofDeadlineBlock,
            record.eraseProofHash,
            record.finalizedAt
        );
    }

    function isEraseProofOverdue(bytes32 sigPubKey) external view returns (bool) {
        return _isEraseProofOverdue(sigPubKey);
    }

    function _clearActiveTEEs() internal {
        for(uint256 i = 0; i < activeTEEs.length; i++) {
            uint256 index = activeTEEs[i];
            candidateTEEs[index].indexInActiveList = 0;
        }
        delete activeTEEs;
    }

   function _isActiveTEE(bytes32 sigPubKey) internal view returns (bool) {
        require(isSigPubKeyRegistered[sigPubKey], "Signature public key not registered");
        return candidateTEEs[indexOfSigPubKey[sigPubKey]].status == TEEStatus.Active;
   }

   function _isCurrentEpochActiveTEEOwner(address caller) internal view returns (bool) {
        bytes32 sigPubKey = sigPubKeyOfOwner[caller];
        if (sigPubKey == bytes32(0) || !isSigPubKeyRegistered[sigPubKey]) {
            return false;
        }
        return candidateTEEs[indexOfSigPubKey[sigPubKey]].status == TEEStatus.Active;
   }

   function _verifyAndDeriveVRFResult(
        uint256 epoch,
        bytes32 sigPubKey,
        bytes32 vrfPubKey,
        bytes32 seed,
        bytes calldata vrfProof
   ) internal view returns (bytes32) {
        require(vrfProof.length == 65, "Invalid VRF proof length");
        require(uint256(vrfPubKey) <= type(uint160).max, "VRF public key must be address");

        bytes32 r;
        bytes32 s;
        uint8 v;
        assembly {
            r := calldataload(vrfProof.offset)
            s := calldataload(add(vrfProof.offset, 32))
            v := byte(0, calldataload(add(vrfProof.offset, 64)))
        }
        if (v < 27) {
            v += 27;
        }
        require(v == 27 || v == 28, "Invalid VRF recovery id");
        require(uint256(s) <= SECP256K1_N_DIV_2, "Invalid VRF signature");

        bytes32 vrfDigest = computeVRFDigest(epoch, sigPubKey, vrfPubKey, seed);
        address recovered = ecrecover(vrfDigest, v, r, s);
        require(recovered != address(0) && recovered == address(uint160(uint256(vrfPubKey))), "Invalid VRF proof");

        return keccak256(abi.encodePacked(VRF_DOMAIN, seed, vrfPubKey, r, s, v));
   }

   function getActiveTEEs() external view returns (bytes32[] memory) {
        bytes32[] memory activeSigPubKeys = new bytes32[](activeTEEs.length);
        for(uint256 i = 0; i < activeTEEs.length; i++) {
            activeSigPubKeys[i] = candidateTEEs[activeTEEs[i]].sigPubKey;
        }
        return activeSigPubKeys;
   }

   function getCandidateTEEs() external view returns (bytes32[] memory) {
        uint256 candidateCount = 0;
        for(uint i=0;i < candidateTEEs.length; i++){
            if (candidateTEEs[i].status == TEEStatus.Candidate) {
                candidateCount++;
            }
        }

        bytes32[] memory SigPubKeys = new bytes32[](candidateCount);
        uint256 cursor = 0;
        for(uint i=0;i < candidateTEEs.length; i++){
            if (candidateTEEs[i].status == TEEStatus.Candidate) {
                SigPubKeys[cursor] = candidateTEEs[i].sigPubKey;
                cursor++;
            }
        }
        return SigPubKeys;
   }

    function getTEEInfo(bytes32 sigPubKey) external view returns (bytes32, bytes32, address, uint256, bool) {
        require(isSigPubKeyRegistered[sigPubKey], "Signature public key not registered");
        TEE memory tee = candidateTEEs[indexOfSigPubKey[sigPubKey]];
        return (tee.MR_ENCLAVE, tee.sigPubKey, tee.owner, tee.deposit, tee.status == TEEStatus.Active);
   }

    function getTEEEnclavePK(bytes32 sigPubKey) external view returns (bytes32[2] memory) {
        require(isSigPubKeyRegistered[sigPubKey], "Signature public key not registered");
        return candidateTEEs[indexOfSigPubKey[sigPubKey]].enclavePK;
   }

    function getPendingTEE(address _owner) external view returns (bool, uint256, uint256, uint256, bytes32) {
        if (!_hasPendingTEE(_owner)) {
            return (false, 0, 0, 0, bytes32(0));
        }

        PendingTEE memory pending = pendingTEEOfOwner[_owner];
        return (true, pending.deposit, pending.nonce, 0, pending.challengeHash);
    }

    function getRegistrationChallenge(address _owner) external view returns (bytes32) {
        if (!_hasPendingTEE(_owner)) {
            return bytes32(0);
        }
        return pendingTEEOfOwner[_owner].challengeHash;
    }

    function getTEEStatusByOwner(address _owner) external view returns (uint8, bytes32) {
        (TEEStatus status, bytes32 registeredSigPubKey) = _getTEEStatus(_owner);
        return (uint8(status), registeredSigPubKey);
    }

    function setPkCH(bytes calldata _pkCH, bytes calldata signature) external rotateIfNeeded onlyCurrentEpochActiveTEE {
        require(_pkCH.length > 0, "pkCH required");
        require(pkCH.length == 0, "pkCH already initialized");
        require(tT > 0 && activeTEEs.length >= tT, "Active TEE threshold not met");
        bytes32 sigPubKey = sigPubKeyOfOwner[msg.sender];
        TEE storage tee = candidateTEEs[indexOfSigPubKey[sigPubKey]];
        bytes32 pkCHDigest = computePkCHDigest(msg.sender, _pkCH);
        require(_verifyTEESignature(pkCHDigest, tee.sigPubKey, signature), "Invalid pkCH signature");
        pkCH = _pkCH;
        emit PkCHSet(pkCHVersion, getPkCHHash());
    }

    function computePkCHDigest(address teeOwner, bytes calldata _pkCH) public view returns (bytes32) {
        return keccak256(abi.encode(
            PKCH_DOMAIN,
            block.chainid,
            address(this),
            teeOwner,
            keccak256(_pkCH)
        ));
    }

    function getPkCHHash() public view returns (bytes32) {
        return keccak256(pkCH);
    }

    function getSystemParams() external view returns (
        uint256 committeeVersion,
        bytes32 pkCHHash,
        uint256 currentPkCHVersion,
        uint256 configuredEpochLength,
        uint256 epoch,
        uint256 activeThreshold,
        uint256 activeLimit
    ) {
        return (committeePubKeyVersion, getPkCHHash(), pkCHVersion, _currentEpochLength(), currentEpoch, tT, nT);
    }

    function computeDReq(ReqFields calldata req) public view returns (bytes32) {
        return keccak256(abi.encode(
            REQ_DOMAIN,
            block.chainid,
            address(this),
            req.version,
            req.epoch,
            req.reqID,
            req.bodyHashes
        ));
    }

    function computeAuthorizationDigest(
        bytes32 dReq,
        bytes32 dPolicy,
        uint256 pubKeyVersion,
        uint256 expiresAt
    ) public pure returns (bytes32) {
        return keccak256(abi.encode(AUTH_DOMAIN, dReq, dPolicy, pubKeyVersion, expiresAt));
    }

    function computeRejectionDigest(
        bytes32 dReq,
        bytes32 dPolicy,
        bytes32 reasonHash,
        uint256 pubKeyVersion
    ) public pure returns (bytes32) {
        return keccak256(abi.encode(REJECT_DOMAIN, dReq, dPolicy, reasonHash, pubKeyVersion));
    }

    function computeRevocationDigest(
        bytes32 sigPubKey,
        bytes32 reasonHash,
        uint256 slashAmount,
        uint256 pubKeyVersion
    ) public view returns (bytes32) {
        return keccak256(abi.encode(REVOKE_DOMAIN, block.chainid, address(this), sigPubKey, reasonHash, slashAmount, pubKeyVersion));
    }

    function computeRedactionProofHash(
        bytes32 reqID,
        bytes32 dReq,
        bytes32 activeSetHash,
        bytes calldata piReqA
    ) public pure returns (bytes32) {
        return keccak256(abi.encode(reqID, dReq, activeSetHash, piReqA));
    }

    function computeShareProofDigest(bytes32 dReq, bytes calldata sI) public pure returns (bytes32) {
        return keccak256(abi.encodePacked(dReq, sI));
    }

    function computeEraseProofDigest(
        bytes32 sigPubKey,
        address teeOwner,
        uint256 epoch,
        bytes32 eraseProofHash
    ) public view returns (bytes32) {
        return keccak256(abi.encode(ERASE_DOMAIN, block.chainid, address(this), sigPubKey, teeOwner, epoch, eraseProofHash));
    }

    function computeSeed(bytes32 finalityBlockHash, uint256 epoch) public view returns (bytes32) {
        return keccak256(abi.encodePacked(finalityBlockHash, epoch, address(this)));
    }

    function computeVRFDigest(
        uint256 epoch,
        bytes32 sigPubKey,
        bytes32 vrfPubKey,
        bytes32 seed
    ) public view returns (bytes32) {
        return keccak256(abi.encode(VRF_DOMAIN, block.chainid, address(this), epoch, sigPubKey, vrfPubKey, seed));
    }

    function isSystemReady() public view returns (bool) {
        return pkCH.length > 0 &&
            tT > 0 &&
            nT >= tT &&
            activeTEEs.length >= tT;
    }

    function submitRedactAuthorization(
        ReqFields calldata req,
        bytes32 dPolicy,
        uint256 expiresAt,
        uint256[2] calldata signature
    ) external rotateIfNeeded returns (bool) {
        require(isSystemReady(), "System not ready");
        require(req.version == REDACT_PROTOCOL_VERSION, "Unsupported redact protocol");
        require(req.reqID != bytes32(0), "reqID required");
        require(redactRequests[req.reqID].status == RedactStatus.None, "Redact request exists");
        require(uint256(req.epoch) == currentEpoch, "Epoch mismatch");
        require(req.bodyHashes[5] == activeSetHashByEpoch[uint256(req.epoch)], "Active set hash mismatch");
        require(req.bodyHashes[6] == getPkCHHash(), "pkCH hash mismatch");
        require(expiresAt > block.timestamp, "Authorization expired");

        bytes32 dReq = computeDReq(req);
        bytes32 authDigest = computeAuthorizationDigest(dReq, dPolicy, committeePubKeyVersion, expiresAt);
        require(verifyAggregatedSignature(authDigest, signature), "Invalid authorization signature");

        redactRequests[req.reqID] = RedactRequestRecord({
            epoch: uint256(req.epoch),
            dReq: dReq,
            activeSetHash: req.bodyHashes[5],
            status: RedactStatus.Authorized,
            expiresAt: expiresAt
        });

        emit RedactRequestPayloadRecorded(req.reqID, req.bodyHashes[0], req.bodyHashes[1], req.bodyHashes[2], req.bodyHashes[3], req.bodyHashes[4], req.bodyHashes[5], req.bodyHashes[6]);
        emit RedactAuthorized(req.reqID, uint256(req.epoch), dReq, dPolicy, req.bodyHashes[5], req.bodyHashes[6], committeePubKeyVersion, expiresAt, signature);
        return true;
    }

    function rejectRedactRequest(
        ReqFields calldata req,
        bytes32 dPolicy,
        bytes32 reasonHash,
        uint256[2] calldata signature
    ) external rotateIfNeeded returns (bool) {
        require(req.version == REDACT_PROTOCOL_VERSION, "Unsupported redact protocol");
        require(req.reqID != bytes32(0), "reqID required");
        require(reasonHash != bytes32(0), "Reason hash required");
        require(redactRequests[req.reqID].status == RedactStatus.None, "Redact request exists");
        require(uint256(req.epoch) == currentEpoch, "Epoch mismatch");

        bytes32 dReq = computeDReq(req);
        bytes32 rejectDigest = computeRejectionDigest(dReq, dPolicy, reasonHash, committeePubKeyVersion);
        require(verifyAggregatedSignature(rejectDigest, signature), "Invalid rejection signature");

        redactRequests[req.reqID] = RedactRequestRecord({
            epoch: uint256(req.epoch),
            dReq: dReq,
            activeSetHash: req.bodyHashes[5],
            status: RedactStatus.Rejected,
            expiresAt: 0
        });

        emit RedactRequestPayloadRecorded(req.reqID, req.bodyHashes[0], req.bodyHashes[1], req.bodyHashes[2], req.bodyHashes[3], req.bodyHashes[4], req.bodyHashes[5], req.bodyHashes[6]);
        emit RedactRejected(req.reqID, dReq, dPolicy, reasonHash, signature);
        return true;
    }

    function executeRedaction(
        bytes32 reqID,
        bytes32 rPrimeHash,
        bytes32 proofHash,
        bytes32 resultHash,
        bytes calldata piReqA
    ) external rotateIfNeeded returns (bool) {
        RedactRequestRecord storage req = redactRequests[reqID];
        require(req.status == RedactStatus.Authorized, "Redact request not authorized");
        if (block.timestamp > req.expiresAt) {
            req.status = RedactStatus.Expired;
            emit RedactExpired(reqID, req.expiresAt);
            return false;
        }
        require(rPrimeHash != bytes32(0), "rPrime hash required");
        require(proofHash != bytes32(0), "Proof hash required");
        require(resultHash != bytes32(0), "Result hash required");
        require(piReqA.length > 0, "pi_req_A required");
        _verifyPiReqA(req.dReq, piReqA);
        require(
            proofHash == computeRedactionProofHash(reqID, req.dReq, req.activeSetHash, piReqA),
            "Proof hash mismatch"
        );

        bytes32 piReqAHash = keccak256(piReqA);
        req.status = RedactStatus.Executed;

        emit RedactExecuted(reqID, rPrimeHash, proofHash, resultHash, piReqAHash, piReqA);
        return true;
    }

    function getRedactRequest(bytes32 reqID) external view returns (
        bytes32 storedReqID,
        uint256 epoch,
        bytes32 dReq,
        bytes32 dPolicy,
        uint8 status,
        uint256 authorizedAt,
        uint256 executedAt,
        uint256 expiresAt,
        bytes32 rejectReasonHash
    ) {
        RedactRequestRecord storage req = redactRequests[reqID];
        bytes32 recordReqID = req.status == RedactStatus.None ? bytes32(0) : reqID;
        return (recordReqID, req.epoch, req.dReq, bytes32(0), _effectiveRedactStatus(req), 0, 0, req.expiresAt, bytes32(0));
    }

    function isRedactionExecuted(bytes32 reqID) external view returns (bool) {
        return redactRequests[reqID].status == RedactStatus.Executed;
    }

    function getEffectiveRedactStatus(bytes32 reqID) external view returns (uint8) {
        return _effectiveRedactStatus(redactRequests[reqID]);
    }

    function getActiveSet(uint256 epoch) external view returns (bytes32[] memory) {
        if (epoch != currentEpoch) {
            return new bytes32[](0);
        }
        return _activeSigPubKeys();
    }

    function _isCurrentActiveSigPubKey(bytes32 sigPubKey) internal view returns (bool) {
        for (uint256 i = 0; i < activeTEEs.length; i++) {
            if (candidateTEEs[activeTEEs[i]].sigPubKey == sigPubKey) {
                return true;
            }
        }
        return false;
    }

    function _verifyPiReqA(bytes32 dReq, bytes calldata piReqA) internal view {
        ShareProof[] memory proofs = abi.decode(piReqA, (ShareProof[]));
        require(proofs.length >= tT, "TEE proof threshold not met");

        for (uint256 i = 0; i < proofs.length; i++) {
            require(proofs[i].sigPubKey != bytes32(0), "TEE sig pubkey required");
            require(proofs[i].sI.length > 0, "TEE share required");
            require(_isCurrentActiveSigPubKey(proofs[i].sigPubKey), "TEE sig pubkey not active");

            for (uint256 j = 0; j < i; j++) {
                require(proofs[j].sigPubKey != proofs[i].sigPubKey, "Duplicate TEE sig pubkey");
            }

            bytes32 shareDigest = keccak256(abi.encodePacked(dReq, proofs[i].sI));
            require(_verifyTEESignature(shareDigest, proofs[i].sigPubKey, proofs[i].sigma), "Invalid TEE share signature");
        }
    }

    function getActiveSetHash(uint256 epoch) external view returns (bytes32) {
        return activeSetHashByEpoch[epoch];
    }

    function _activeSigPubKeys() internal view returns (bytes32[] memory) {
        bytes32[] memory activeSigPubKeys = new bytes32[](activeTEEs.length);
        for(uint256 i = 0; i < activeTEEs.length; i++) {
            activeSigPubKeys[i] = candidateTEEs[activeTEEs[i]].sigPubKey;
        }
        return activeSigPubKeys;
    }

    function _checkpointActiveSet(uint256 epoch) internal {
        bytes32[] memory sigPubKeys = _activeSigPubKeys();
        bytes32 setHash = _hashActiveSet(sigPubKeys);
        activeSetHashByEpoch[epoch] = setHash;
        emit ActiveSetCheckpointed(epoch, setHash, sigPubKeys);
    }

    function _rotationDeadlineBlock() internal view returns (uint256) {
        return lastRotationBlock + _currentEpochLength();
    }

    function _currentEpochLength() internal view returns (uint256) {
        if (currentEpoch == 0 || steadyEpochLength == 0) {
            return epochLength;
        }
        return steadyEpochLength;
    }

    function _isEraseProofOverdue(bytes32 sigPubKey) internal view returns (bool) {
        TEEExitRecord storage record = teeExitRecords[sigPubKey];
        return record.pending &&
            record.eraseProofDeadlineBlock != 0 &&
            block.number > record.eraseProofDeadlineBlock;
    }

    function _slashExpiredEraseProofsIfNeeded() internal {
        uint256 slashedCount = 0;
        for (uint256 i = 0; i < candidateTEEs.length && slashedCount < MAX_ERASE_TIMEOUT_SLASHES_PER_CALL; i++) {
            bytes32 sigPubKey = candidateTEEs[i].sigPubKey;
            if (
                candidateTEEs[i].status == TEEStatus.Exited &&
                candidateTEEs[i].deposit > 0 &&
                _isEraseProofOverdue(sigPubKey)
            ) {
                uint256 slashAmount = candidateTEEs[i].deposit;
                candidateTEEs[i].deposit = 0;
                candidateTEEs[i].status = TEEStatus.Revoked;
                teeExitRecords[sigPubKey].pending = false;
                emit TEERevoked(sigPubKey, candidateTEEs[i].owner, ERASE_TIMEOUT_REASON, block.timestamp, slashAmount);
                slashedCount++;
            }
        }
    }

    function _isRotationDue() internal view returns (bool) {
        uint256 currentLength = _currentEpochLength();
        return currentLength > 0 && block.number >= lastRotationBlock + currentLength;
    }

    function _rotationSeed(uint256 epoch) internal view returns (uint256 finalityBlockNumber, bytes32 finalityBlockHash, bytes32 seed) {
        if (block.number <= FINALITY_BLOCK_DELAY) {
            return (0, bytes32(0), bytes32(0));
        }

        finalityBlockNumber = block.number - FINALITY_BLOCK_DELAY;
        finalityBlockHash = blockhash(finalityBlockNumber);
        seed = computeSeed(finalityBlockHash, epoch);
    }

    function _requestRotationPreparationIfNeeded() internal returns (bool) {
        // pkCH/system 未就位时不触发新轮换:bootstrap 前 lastRotationBlock 仍是
        // genesis 值,预备窗口会按"抢跑的死线"提前触发 VRF,而此时委员会密钥尚不存在。
        if (!isSystemReady()) {
            return false;
        }
        if (!isRotationPreparationPhase()) {
            return false;
        }

        uint256 nextEpoch = currentEpoch + 1;
        if (rotationPreparationRequested[nextEpoch]) {
            return true;
        }

        (uint256 finalityBlockNumber, bytes32 finalityBlockHash, bytes32 seed) = _rotationSeed(nextEpoch);
        if (finalityBlockHash == bytes32(0)) {
            return false;
        }

        rotationPreparationRequested[nextEpoch] = true;
        finalityBlockByEpoch[nextEpoch] = finalityBlockNumber;
        finalityBlockHashByEpoch[nextEpoch] = finalityBlockHash;
        seedByEpoch[nextEpoch] = seed;
        emit TEERotationPreparationRequested(
            currentEpoch,
            nextEpoch,
            _rotationDeadlineBlock(),
            _eligibleRotationTEECount(),
            finalityBlockNumber,
            finalityBlockHash,
            seed
        );
        return true;
    }

    function _rotateActiveSetIfNeeded() internal {
        if (!initialActiveSetBootstrapped && activeTEEs.length == 0) {
            return;
        }
        if (!_isRotationDue()) {
            return;
        }

        uint256 currentLength = _currentEpochLength();
        uint256 nextEpoch = currentEpoch + 1;
        bytes32 seed = seedByEpoch[nextEpoch];
        if (seed == bytes32(0)) {
            return;
        }
        uint256 submissionCount = _vrfSubmissionCount(nextEpoch);
        if (submissionCount < nT) {
            emit ActiveSetRotationDeferred(nextEpoch, submissionCount, nT);
            return;
        }

        lastRotationBlock = lastRotationBlock + currentLength;
        lastRotationTimestamp = block.timestamp;

        uint256 previousEpoch = currentEpoch;
        uint256[] memory previousActiveTEEs = new uint256[](activeTEEs.length);
        for (uint256 i = 0; i < activeTEEs.length; i++) {
            previousActiveTEEs[i] = activeTEEs[i];
        }

        _clearActiveTEEs();
        uint256 selectedCount = _selectActiveTEEs(nextEpoch);
        require(selectedCount == nT, "Active set underfilled");
        for (uint256 i = 0; i < selectedCount; i++) {
            uint256 teeIndex = activeTEEs[i];
            candidateTEEs[teeIndex].status = TEEStatus.Active;
            candidateTEEs[teeIndex].indexInActiveList = i;
        }
        if (selectedCount > 0) {
            initialActiveSetBootstrapped = true;
        }
        currentEpoch = nextEpoch;
        _checkpointActiveSet(currentEpoch);
        emit ActiveSetRotated(
            currentEpoch,
            activeSetHashByEpoch[currentEpoch],
            block.timestamp,
            finalityBlockByEpoch[currentEpoch],
            finalityBlockHashByEpoch[currentEpoch],
            seed,
            _activeSigPubKeys()
        );

        for (uint256 i = 0; i < previousActiveTEEs.length; i++) {
            uint256 teeIndex = previousActiveTEEs[i];
            if (candidateTEEs[teeIndex].status != TEEStatus.Active || _isSelectedTEEIndex(teeIndex, selectedCount)) {
                continue;
            }
            candidateTEEs[teeIndex].status = TEEStatus.Exited;
            uint256 eraseProofDeadlineBlock = block.number + eraseProofGraceBlocks;
            bytes32 sigPubKey = candidateTEEs[teeIndex].sigPubKey;
            address teeOwner = candidateTEEs[teeIndex].owner;
            teeExitRecords[sigPubKey] = TEEExitRecord({
                pending: true,
                owner: teeOwner,
                exitEpoch: previousEpoch,
                eraseProofDeadlineBlock: eraseProofDeadlineBlock,
                eraseProofHash: bytes32(0),
                finalizedAt: 0
            });
            emit TEEExitPending(sigPubKey, teeOwner, previousEpoch, nextEpoch, block.timestamp);
            emit TEEEraseProofDeadlineSet(sigPubKey, teeOwner, previousEpoch, eraseProofDeadlineBlock);
        }
    }

    function _bootstrapInitialActiveSetIfReady() internal {
        if (initialActiveSetBootstrapped || activeTEEs.length != 0 || nT == 0 || tT == 0 || nT < tT) {
            return;
        }
        if (_eligibleRotationTEECount() < nT) {
            return;
        }

        uint256 selectedCount = 0;
        for (uint256 i = 0; i < candidateTEEs.length && selectedCount < nT; i++) {
            if (candidateTEEs[i].status != TEEStatus.Candidate) {
                continue;
            }
            candidateTEEs[i].status = TEEStatus.Active;
            candidateTEEs[i].indexInActiveList = activeTEEs.length;
            activeTEEs.push(i);
            selectedCount++;
        }

        if (selectedCount == nT) {
            initialActiveSetBootstrapped = true;
            lastRotationTimestamp = block.timestamp;
            lastRotationBlock = block.number;
            _checkpointActiveSet(currentEpoch);
            emit ActiveSetRotated(
                currentEpoch,
                activeSetHashByEpoch[currentEpoch],
                block.timestamp,
                0,
                bytes32(0),
                bytes32(0),
                _activeSigPubKeys()
            );
        }
    }

    function _eligibleRotationTEECount() internal view returns (uint256 count) {
        for (uint256 i = 0; i < candidateTEEs.length; i++) {
            if (candidateTEEs[i].status == TEEStatus.Candidate || candidateTEEs[i].status == TEEStatus.Active) {
                count++;
            }
        }
    }

    function _vrfSubmissionCount(uint256 epoch) internal view returns (uint256 count) {
        for (uint256 i = 0; i < candidateTEEs.length; i++) {
            if (candidateTEEs[i].status != TEEStatus.Candidate && candidateTEEs[i].status != TEEStatus.Active) {
                continue;
            }
            if (vrfSubmissions[epoch][candidateTEEs[i].sigPubKey].exists) {
                count++;
            }
        }
    }

    function _isSelectedTEEIndex(uint256 teeIndex, uint256 selectedCount) internal view returns (bool) {
        for (uint256 i = 0; i < selectedCount; i++) {
            if (activeTEEs[i] == teeIndex) {
                return true;
            }
        }
        return false;
    }

    function _isBetterVRFScore(uint256 score, bytes32 sigPubKey, uint256 currentScore, bytes32 currentSigPubKey) internal pure returns (bool) {
        return score > currentScore || (score == currentScore && sigPubKey > currentSigPubKey);
    }

    function _isWorseVRFScore(uint256 score, bytes32 sigPubKey, uint256 currentScore, bytes32 currentSigPubKey) internal pure returns (bool) {
        return score < currentScore || (score == currentScore && sigPubKey < currentSigPubKey);
    }

    function _selectActiveTEEs(uint256 epoch) internal returns (uint256 selectedCount) {
        if (nT == 0) {
            return 0;
        }

        uint256[] memory selectedIndexes = new uint256[](nT);
        uint256[] memory selectedScores = new uint256[](nT);
        bytes32[] memory selectedSigPubKeys = new bytes32[](nT);

        for (uint256 i = 0; i < candidateTEEs.length; i++) {
            if (candidateTEEs[i].status != TEEStatus.Candidate && candidateTEEs[i].status != TEEStatus.Active) {
                continue;
            }

            VRFSubmission storage submission = vrfSubmissions[epoch][candidateTEEs[i].sigPubKey];
            if (!submission.exists) {
                continue;
            }

            uint256 score = uint256(submission.vrfResult);
            if (selectedCount < nT) {
                selectedIndexes[selectedCount] = i;
                selectedScores[selectedCount] = score;
                selectedSigPubKeys[selectedCount] = candidateTEEs[i].sigPubKey;
                selectedCount++;
            } else {
                uint256 worstIndex = 0;
                for (uint256 j = 1; j < selectedCount; j++) {
                    if (_isWorseVRFScore(selectedScores[j], selectedSigPubKeys[j], selectedScores[worstIndex], selectedSigPubKeys[worstIndex])) {
                        worstIndex = j;
                    }
                }
                if (_isBetterVRFScore(score, candidateTEEs[i].sigPubKey, selectedScores[worstIndex], selectedSigPubKeys[worstIndex])) {
                    selectedIndexes[worstIndex] = i;
                    selectedScores[worstIndex] = score;
                    selectedSigPubKeys[worstIndex] = candidateTEEs[i].sigPubKey;
                }
            }
        }

        for (uint256 i = 1; i < selectedCount; i++) {
            uint256 indexKey = selectedIndexes[i];
            uint256 scoreKey = selectedScores[i];
            bytes32 sigPubKey = selectedSigPubKeys[i];
            uint256 j = i;
            while (
                j > 0 &&
                _isBetterVRFScore(scoreKey, sigPubKey, selectedScores[j - 1], selectedSigPubKeys[j - 1])
            ) {
                selectedIndexes[j] = selectedIndexes[j - 1];
                selectedScores[j] = selectedScores[j - 1];
                selectedSigPubKeys[j] = selectedSigPubKeys[j - 1];
                j--;
            }
            selectedIndexes[j] = indexKey;
            selectedScores[j] = scoreKey;
            selectedSigPubKeys[j] = sigPubKey;
        }

        for (uint256 i = 0; i < selectedCount; i++) {
            activeTEEs.push(selectedIndexes[i]);
        }
        return selectedCount;
    }

    function _effectiveRedactStatus(RedactRequestRecord storage req) internal view returns (uint8) {
        if (req.status == RedactStatus.Authorized && req.expiresAt != 0 && block.timestamp > req.expiresAt) {
            return uint8(RedactStatus.Expired);
        }
        return uint8(req.status);
    }

    function _hashActiveSet(bytes32[] memory hardwareIds) internal pure returns (bytes32) {
        for (uint256 i = 1; i < hardwareIds.length; i++) {
            bytes32 key = hardwareIds[i];
            uint256 j = i;
            while (j > 0 && hardwareIds[j - 1] > key) {
                hardwareIds[j] = hardwareIds[j - 1];
                j--;
            }
            hardwareIds[j] = key;
        }

        bytes memory packed;
        for (uint256 i = 0; i < hardwareIds.length; i++) {
            packed = abi.encodePacked(packed, hardwareIds[i]);
        }
        return keccak256(packed);
    }

}
