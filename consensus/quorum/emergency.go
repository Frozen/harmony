package quorum

import (
	"encoding/hex"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/harmony-one/harmony/consensus/votepower"
	"github.com/harmony-one/harmony/crypto/bls"
	"github.com/harmony-one/harmony/internal/params"
	"github.com/harmony-one/harmony/numeric"
	"github.com/harmony-one/harmony/shard"
	"github.com/pkg/errors"
)

const (
	emergencyShard0Epoch         = uint64(3002)
	emergencyShard0Block         = uint64(92733439)
	emergencyShard0CommitteeSize = 197
)

var (
	// The override is limited to the certificate for the last block of epoch 3002.
	// Keys keep their canonical bitmap positions; only consensus voting power changes.
	// The retained epoch-3002 shard-0 roster has 197 slots; this manifest matches
	// 41 of them with canonical power 0.190719138817442899.
	// Manifest SHA-256 (concatenated 48-byte keys):
	// c287ca243feb79036ddc55fcefa6fcc8de3e97b4b9d9aeeeb2750d3cfc12f799.
	emergencyShard0ParentHash          = common.HexToHash("0xcbaceb7635b4e2d612c21b34fe24f308076e25b02d6379be17150f77b86f8f32")
	emergencyShard0ExcludedKeyManifest = serializedPublicKeys([]string{
		// Infinity Tech: one1slf58d4kaus4h9st228dxagqcv5afluwzuj0nd (9 slots).
		"51a71f4bfbe4b6bdbec32e80bcde1afd6db948bbd2da83890632fc62412b3be481867f3f45b385c059b3d704e879b104",
		"c03555b6c4ac4070de7db9c0466aba226351eb388a51a36d708334523d1bb62c1a4194efd1107a780f580f3dd877fb84",
		"3030cf71580f58566ad833c27a3c6f45084f7c2170cdc169023b2325a56be15fb1ad074f5c729f45bf429d24033da48c",
		"e8c6f0b5f7f75b9d33b4703261995cd722b15b3d9d9112bfe0bfb1814a7d6d21c475fdfb1e58c7779fe1b6cb15412f10",
		"469eac031cd383331d5b2921c19643546b2a496ccf67fcfce97df162926b0856d95430b5cda1ae29273a663abcf50e98",
		"413f61cbcf6906d88313f6ca4b2df37e3ac6f5bda4cc4a1af9998a2eabe78137ffd2e53e633f9d82712b173534c9c480",
		"8bd3c37177d44bbe4b5fc5fab1db04ca7472afc47147be30ca0cbc59e9fd24cbcedbe53ca4d44f0ce43d481258a5ef80",
		"b8e944935f835ed9ed661d49a62ed66331e8576f669405694489d17d908dbdf0c18697454c65767de8519b4cd5229a08",
		"99778cbc242493728de3e12033b5ee93ed81e9281125c827e344dc7ebcb6a658e9a27b445ad880e5774b4b7d08038404",
		// KryptoKnight: one1sw72xy472qsxqx7vesmy0va6wz5feukl2y4nps (10 slots).
		"f80ef7f64df59814cf325d57d8326634c19a42601cd1b6ae308b82ef8cf6d4254e4c19823978d9b249e9119540f13710",
		"85fb01b875c3397b39cf6dcaa0b0185e165834306efb1332db69829eb86da9a62f2726d96eae05c18e68159c8282d104",
		"912ca75b070b8d86cd3c57bdc6c60ff918f339c484b3e6d55498e1df635e620cdbaef01b77121dbffcd54a7531d14798",
		"58b09e61da62dbce3f15b1952253a7d76219f665a75592592a1353cdb4990d4e38f8f4e056e3f506cbfabc7c9bd13418",
		"8cf586c8ee5ebdaa390084c9e53e7e9070ed1484ddeb41769c2ee607ff97967ee246499efa47e1dffa53acbd76c9f090",
		"5ff530ce335a6859eb3cb51a9983525a2908691c76791e9e9e0f44ab12263a0419e8c15fe0c5dbc371f87e2a7f9af590",
		"6574eff9ae677d34a7915a2f87f56e65982fbc8c9731fd47cc6d41a1e6a8c3629a77e8ee943df7c3afbd8e5d40226a84",
		"41a46e0d698c2739aee4650da79385054447abae23f209053d2e951fc361d79408b7942362c5a7f755a66e5036be9884",
		"35a8facd18291acc8a35ab400144b3fe4256230ef5ed0db5971859f753e7744c2c45dd8514225b12b21b142cfa81d98c",
		"7f077da652470e0eaab4ae5de7a96e6aab8600252063f20235c5a4157fdcc4604aec89fd802c32c43ba81d433a03c900",
		// StrongMindsHold: one169pe3q35m7g30a965q94lr222mtyu4h2jdtwm9 (11 slots).
		"3e0840afb7ab02a63cfddd62d61f447d201d14f35280a269fd5f7ab921f7d77361b446b56696d1a3824a11c56f53a800",
		"4fb3ab0257a43e5c7fd477e0ccf636cba858f4a0331fa49c6195bbc7e9f5c5ca3cab56f45a38fd9f5f00b906f9ebc294",
		"6ed32a74cace2e5e41fde1175113f32d12a1fffac0bbf0cfaf00ccf378e84b32da2116083aa7e92f205ce378db750e8c",
		"6f49fd414ba66dde70fd3c153977389f3f06fe2ffb9ee8c3b307f91b21553772d1875f3f2b78c2043ad27ce0ca4a980c",
		"8ffd1bc98db7ad0264925aa1a01dd392836fc02e75e0c69db55f3b2e8992325c8feedeacc5c792045dd12af60a201e18",
		"11bcddd206039eeb3b43e4912ca40f668338da42ee15b569f38a67225ba074b7553775da1df1fe854964c34f366d6f84",
		"38ba2352a1766734f00907c8e4e02aa938376d4301f643dfcfc93bbdfcda8e49a070204f8e7227293c41f754209a0f80",
		"212d9d6f0d81312e39a9d22e5d33905e8127d7ff29f063d3f6c957deb17c0fed45ea437330a3791ab43b53eb6e997e88",
		"3742c8a178ec82a1b6f29cc45f4671cbb61c4b0acc9ebd17419023d4a113648d4ed9f8d9e7add9605152a81720bd3c88",
		"8297a46c9e28c034b3d6ba5d793b506b6652fae76b370e0a37cd8e78f7a4eb12e1c87aa33ff3caf1781511f78e320500",
		"0989c8a4aeb25555338f835b55d11d901079578075473de09a0624f735e12434c49ea86112767cb235bab5b11d8a7100",
		// RockTheBlockchain: one1p2rmvndevvw682qynqu08hyvx24hh4runsw6pz (11 slots).
		"0594316786cb8cb0c5a11a3f13918e96f03b773bd760e499320d6aeb70fd45444c40d11aa533f01a07edd09536e20f18",
		"330237de666c04770198a1369e7c774931ed039dfd0ca57fe6cedcbf38c24ae6b3835816ad1ad1f92e88679f37be1b94",
		"47233341bd1beeac4b9aabdb3e1dd587ccccef06701f1dd2bc7259dd20c0c9571b0e32f1e7d771f6a148ce7c753b0300",
		"4fd69f657942fa6321223a46d98dfaf7469783a284b7df93bc783f401d706aa642ab85a84e50d3e5e021c24329c88984",
		"618555d20bb143151db93dbd9709e8e52d925f68e4c2fdcb47fe398a2e73f2563956ad7176d999bc633db7b1420d1790",
		"62a6cb0e686105fa9b06e50afdf8796a4e3e4ebf64415392e22e51d33816a182d0589fc24e29703974add5069803dc14",
		"6a28fb2c13eda4cfbb14806e32f22401a414065c6b9a34e1ec8f6cb6579f31fc40df797d5336b4887b65d00625088788",
		"748bfd182cd233c77b65a1bf64b7fbcc657c191e603303753c7f608cbcec02d7551a8d18372bf2d17736f49688bcbb14",
		"85d62acb3b704586287c5f0385ef2522e40f6678c9b0e88dd1cd197240d038d321ade0275db8ea0b607ea9e58740aa08",
		"986275e6b1b896f9172fc94f1c84bd09886f58d2abfa8c63faf519a430cdb794b353608b2695970db4bf76bff7efe694",
		"fdf03971dca91d25eb202967000d555b1b161a932fd25df36a1556205dd1c42432df0902cff468bb35b3cad51e7b018c",
	})
	emergencyShard0ExcludedKeys = serializedPublicKeySet(emergencyShard0ExcludedKeyManifest)
)

// VotingPowerContext identifies the signed block whose quorum is being formed or verified.
type VotingPowerContext struct {
	ChainID     *big.Int
	BlockNumber uint64
	ParentHash  common.Hash
}

// RequiresVotingPowerRefreshAfterBlock reports whether committing block requires
// rebuilding the live decider for the emergency rule on its child.
func RequiresVotingPowerRefreshAfterBlock(chainID *big.Int, shardID uint32, blockNumber uint64, blockHash common.Hash) bool {
	return chainID != nil && chainID.Cmp(params.MainnetChainID) == 0 &&
		shardID == shard.BeaconChainShardID &&
		blockNumber == emergencyShard0Block-1 &&
		blockHash == emergencyShard0ParentHash
}

func emergencyVotingPowerCandidate(roster *votepower.Roster, ctx VotingPowerContext) bool {
	return roster != nil && ctx.ChainID != nil &&
		ctx.ChainID.Cmp(params.MainnetChainID) == 0 &&
		roster.ShardID == shard.BeaconChainShardID &&
		ctx.BlockNumber == emergencyShard0Block
}

func emergencyVotingPowerApplies(roster *votepower.Roster, epoch *big.Int, ctx VotingPowerContext) bool {
	return emergencyVotingPowerCandidate(roster, ctx) && epoch != nil && epoch.Uint64() == emergencyShard0Epoch && ctx.ParentHash == emergencyShard0ParentHash
}

func effectiveRosterForContext(roster *votepower.Roster, epoch *big.Int, ctx VotingPowerContext) (*votepower.Roster, error) {
	if !emergencyVotingPowerCandidate(roster, ctx) {
		return roster, nil
	}
	if epoch == nil || epoch.Uint64() != emergencyShard0Epoch {
		return nil, errors.Errorf("unexpected emergency epoch %v, want %d", epoch, emergencyShard0Epoch)
	}
	if ctx.ParentHash != emergencyShard0ParentHash {
		return nil, errors.Errorf("unexpected emergency parent hash %s, want %s", ctx.ParentHash.Hex(), emergencyShard0ParentHash.Hex())
	}
	effective, err := normalizeVotingPowerExcludingKeys(roster, emergencyShard0ExcludedKeys)
	if err != nil {
		return nil, err
	}
	if len(effective.OrderedSlots) != emergencyShard0CommitteeSize {
		return nil, errors.Errorf("unexpected emergency committee size %d, want %d", len(effective.OrderedSlots), emergencyShard0CommitteeSize)
	}
	return effective, nil
}

func normalizeVotingPowerExcludingKeys(roster *votepower.Roster, excluded map[bls.SerializedPublicKey]struct{}) (*votepower.Roster, error) {
	if roster == nil {
		return nil, errors.New("nil voting-power roster")
	}

	result := *roster
	result.OrderedSlots = append([]bls.SerializedPublicKey(nil), roster.OrderedSlots...)
	result.Voters = make(map[bls.SerializedPublicKey]*votepower.AccommodateHarmonyVote, len(roster.Voters))
	result.OurVotingPowerTotalPercentage = roster.OurVotingPowerTotalPercentage.Copy()
	result.TheirVotingPowerTotalPercentage = roster.TheirVotingPowerTotalPercentage.Copy()
	result.TotalEffectiveStake = roster.TotalEffectiveStake.Copy()

	seen := make(map[bls.SerializedPublicKey]struct{}, len(roster.OrderedSlots))
	canonicalTotal := numeric.ZeroDec()
	for _, key := range roster.OrderedSlots {
		if _, duplicate := seen[key]; duplicate {
			return nil, errors.Errorf("duplicate ordered BLS key %s", key.Hex())
		}
		seen[key] = struct{}{}
		voter, ok := roster.Voters[key]
		if !ok {
			return nil, errors.Errorf("ordered BLS key %s missing from roster", key.Hex())
		}
		if voter == nil {
			return nil, errors.Errorf("nil voter for BLS key %s", key.Hex())
		}
		if voter.Identity != key {
			return nil, errors.Errorf("voter identity %s does not match ordered BLS key %s", voter.Identity.Hex(), key.Hex())
		}
		if voter.OverallPercent.IsNil() {
			return nil, errors.Errorf("nil voting power for BLS key %s", key.Hex())
		}
		if voter.OverallPercent.IsNegative() {
			return nil, errors.Errorf("negative voting power for BLS key %s", key.Hex())
		}
		canonicalTotal = canonicalTotal.Add(voter.OverallPercent)

		copyVoter := *voter
		copyVoter.GroupPercent = voter.GroupPercent.Copy()
		copyVoter.EffectiveStake = voter.EffectiveStake.Copy()
		copyVoter.RawStake = voter.RawStake.Copy()
		copyVoter.OverallPercent = voter.OverallPercent.Copy()
		result.Voters[key] = &copyVoter
	}
	if len(seen) != len(roster.Voters) {
		return nil, errors.Errorf("roster size mismatch: %d ordered slots, %d voters", len(seen), len(roster.Voters))
	}
	if !canonicalTotal.Equal(numeric.OneDec()) {
		return nil, errors.Errorf("canonical voting power total %s, want %s", canonicalTotal.String(), numeric.OneDec().String())
	}

	activeTotal := numeric.ZeroDec()
	for key := range excluded {
		if _, ok := result.Voters[key]; !ok {
			return nil, errors.Errorf("missing emergency BLS key %s", key.Hex())
		}
	}
	for _, key := range result.OrderedSlots {
		voter := result.Voters[key]
		if _, removed := excluded[key]; removed {
			voter.OverallPercent = numeric.ZeroDec()
			continue
		}
		activeTotal = activeTotal.Add(voter.OverallPercent)
	}
	if !activeTotal.IsPositive() {
		return nil, errors.Errorf("non-positive remaining voting power %s", activeTotal.String())
	}

	var lastPositive *votepower.AccommodateHarmonyVote
	normalizedTotal := numeric.ZeroDec()
	for _, key := range result.OrderedSlots {
		voter := result.Voters[key]
		if _, removed := excluded[key]; removed {
			continue
		}
		voter.OverallPercent = voter.OverallPercent.Quo(activeTotal)
		if voter.OverallPercent.IsPositive() {
			lastPositive = voter
		}
		normalizedTotal = normalizedTotal.Add(voter.OverallPercent)
	}
	if lastPositive == nil {
		return nil, errors.New("emergency voting-power roster has no positive included voter")
	}
	lastPositive.OverallPercent = lastPositive.OverallPercent.Add(numeric.OneDec().Sub(normalizedTotal))
	if lastPositive.OverallPercent.IsNegative() {
		return nil, errors.New("emergency voting-power residue made the final voter negative")
	}

	result.OurVotingPowerTotalPercentage = numeric.ZeroDec()
	result.TheirVotingPowerTotalPercentage = numeric.ZeroDec()
	finalTotal := numeric.ZeroDec()
	for _, key := range result.OrderedSlots {
		voter := result.Voters[key]
		finalTotal = finalTotal.Add(voter.OverallPercent)
		if voter.IsHarmonyNode {
			result.OurVotingPowerTotalPercentage = result.OurVotingPowerTotalPercentage.Add(voter.OverallPercent)
		} else {
			result.TheirVotingPowerTotalPercentage = result.TheirVotingPowerTotalPercentage.Add(voter.OverallPercent)
		}
	}
	if !finalTotal.Equal(numeric.OneDec()) {
		return nil, errors.Errorf("normalized voting power total %s, want %s", finalTotal.String(), numeric.OneDec().String())
	}
	return &result, nil
}

func serializedPublicKeys(keys []string) []bls.SerializedPublicKey {
	result := make([]bls.SerializedPublicKey, 0, len(keys))
	seen := make(map[bls.SerializedPublicKey]struct{}, len(keys))
	for _, encoded := range keys {
		decoded, err := hex.DecodeString(encoded)
		if err != nil || len(decoded) != bls.PublicKeySizeInBytes {
			panic("invalid emergency BLS public key: " + encoded)
		}
		var key bls.SerializedPublicKey
		copy(key[:], decoded)
		if _, exists := seen[key]; exists {
			panic("duplicate emergency BLS public key: " + encoded)
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}

func serializedPublicKeySet(keys []bls.SerializedPublicKey) map[bls.SerializedPublicKey]struct{} {
	result := make(map[bls.SerializedPublicKey]struct{}, len(keys))
	for _, key := range keys {
		if _, exists := result[key]; exists {
			panic("duplicate emergency BLS public key: " + key.Hex())
		}
		result[key] = struct{}{}
	}
	return result
}
