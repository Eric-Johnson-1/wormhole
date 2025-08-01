package txverifier

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strings"

	"github.com/wormhole-foundation/wormhole/sdk/vaa"
)

// Constants
const (
	MAX_DECIMALS = 8
	// A pair of origin address and origin chain to identify assets moving in and out of the bridge.
	KEY_FORMAT = "%s-%d"

	// CacheMaxSize is the maximum number of entries that should be in any of the caches.
	CacheMaxSize = 100

	// CacheDeleteCount specifies the number of entries to delete from a cache once it reaches CacheMaxSize.
	// Must be less than CacheMaxSize.
	CacheDeleteCount = 10
)

// Extracts the value at the given path from the JSON object, and casts it to
// type T. If the path does not exist in the object, an error is returned.
func extractFromJsonPath[T any](data json.RawMessage, path string) (T, error) {
	var defaultT T

	var obj map[string]interface{}
	err := json.Unmarshal(data, &obj)
	if err != nil {
		return defaultT, err
	}

	// Split the path and iterate over the keys, except for the final key. For
	// each key, check if it exists in the object. If it does exist and is a map,
	// update the object to the value of the key.
	keys := strings.Split(path, ".")
	for _, key := range keys[:len(keys)-1] {
		if obj[key] == nil {
			return defaultT, fmt.Errorf("key %s not found", key)
		}

		if v, ok := obj[key].(map[string]interface{}); ok {
			obj = v
		} else {
			return defaultT, fmt.Errorf("can't convert to key to map[string]interface{} type")
		}
	}

	// If the final key exists in the object, return the value as T. Otherwise,
	// return an error.
	if value, exists := obj[keys[len(keys)-1]]; exists {
		if v, ok := value.(T); ok {
			return v, nil
		} else {
			return defaultT, fmt.Errorf("can't convert to type T")
		}
	} else {
		return defaultT, fmt.Errorf("key %s not found", keys[len(keys)-1])
	}
}

// Normalize the amount to 8 decimals. If the amount has more than 8 decimals,
// the amount is divided by 10^(decimals-8). If the amount has less than 8
// decimals, the amount is returned as is.
// https://wormhole.com/docs/build/start-building/supported-networks/evm/#addresses
func normalize(amount *big.Int, decimals uint8) (normalizedAmount *big.Int) {
	if amount == nil {
		return nil
	}
	if decimals > MAX_DECIMALS {
		exponent := new(big.Int).SetInt64(int64(decimals - 8))
		multiplier := new(big.Int).Exp(new(big.Int).SetInt64(10), exponent, nil)
		normalizedAmount = new(big.Int).Div(amount, multiplier)
	} else {
		return amount
	}

	return normalizedAmount
}

// denormalize() scales an amount to its native decimal representation by multiplying it by some power of 10.
// See also:
//   - documentation:
//     https://github.com/wormhole-foundation/wormhole/blob/main/whitepapers/0003_token_bridge.md#handling-of-token-amounts-and-decimals
//     https://wormhole.com/docs/build/start-building/supported-networks/evm/#addresses
//   - solidity implementation:
//     https://github.com/wormhole-foundation/wormhole/blob/91ec4d1dc01f8b690f0492815407505fb4587520/ethereum/contracts/bridge/Bridge.sol#L295-L300
func denormalize(
	amount *big.Int,
	decimals uint8,
) (denormalizedAmount *big.Int) {
	if decimals > 8 {
		// Scale from 8 decimals to `decimals`
		exponent := new(big.Int).SetInt64(int64(decimals - 8))
		multiplier := new(big.Int).Exp(new(big.Int).SetInt64(10), exponent, nil)
		denormalizedAmount = new(big.Int).Mul(amount, multiplier)

	} else {
		// No scaling necessary
		denormalizedAmount = new(big.Int).Set(amount)
	}

	return denormalizedAmount
}

// SupportedChains returns a slice of Wormhole Chain IDs that have a Transfer Verifier implementation.
func SupportedChains() []vaa.ChainID {
	return []vaa.ChainID{
		// Mainnets
		vaa.ChainIDEthereum,
		// Testnets
		vaa.ChainIDSepolia,
		vaa.ChainIDHolesky,
	}
}

func IsSupported(cid vaa.ChainID) bool {
	return slices.Contains(SupportedChains(), cid)
}

// ValidateChains validates that a slice of uints correspond to chain IDs with a Transfer Verifier implementation.
// Returns a slice of the input values converted into valid, known ChainIDs.
// Returns nil when an error occurs.
func ValidateChains(
	// Uints to be validated. This type is selected because it can be used with Cobra's `UintSlice()` function.
	input []uint,
) ([]vaa.ChainID, error) {
	if len(input) == 0 {
		return nil, errors.New("no chain IDs provided for transfer verification")
	}
	knownChains := vaa.GetAllNetworkIDs()
	supportedChains := SupportedChains()

	// NOTE: Using a known capacity and counter here avoids unnecessary reallocations compared to using `append()`.
	enabled := make([]vaa.ChainID, len(input))
	i := uint8(0)
	for _, chain := range input {
		if chain > uint(math.MaxUint16) {
			return nil, fmt.Errorf("uint %d exceeds MaxUint16", chain)
		}
		chainId := vaa.ChainID(chain)

		if !slices.Contains(knownChains, chainId) {
			return nil, fmt.Errorf("chainId %d is not a known Chain ID", chainId)
		}

		if !slices.Contains(supportedChains, chainId) {
			return nil, fmt.Errorf("chainId %d does not have a Transfer Verifier implementation", chainId)
		}

		enabled[i] = chainId
		i++
	}

	return enabled, nil
}

// deleteEntries deletes CacheDeleteCount entries at random from a pointer to a map.
// Only trims if the length of the cache is greater than CacheMaxSize.
// Returns the number of keys deleted.
func deleteEntries[K comparable, V any](cachePointer *map[K]V) (int, error) {
	if cachePointer == nil {
		return 0, errors.New("nil pointer to map passed to deleteEntries()")
	}

	if *cachePointer == nil {
		return 0, errors.New("nil map passed (pointer points to a nil map)")
	}

	cache := *cachePointer
	currentLen := len(cache)
	// Bounds check
	// NOTE: cache length must be greater than or equal to CacheMaxSize.
	if currentLen <= CacheMaxSize {
		return 0, nil
	}

	// Delete either a fixed number of entries or else delete enough so that the cache
	// will be at CacheMaxSize after the operation.
	deleteCount := max(CacheDeleteCount, currentLen-CacheMaxSize)

	// Allocate array that stores keys to delete.
	keysToDelete := make([]K, deleteCount)

	// Iterate over the map (non-deterministic) and pick some keys to delete.
	var i int
	for key := range cache {
		keysToDelete[i] = key
		i++
		if i >= len(keysToDelete) {
			break
		}
	}

	// Delete the keys from the map.
	for _, key := range keysToDelete {
		delete(cache, key)
	}

	return i, nil
}

// upsert inserts a new key and value into a map or update the value if the key already exists.
func upsert(
	dict *map[string]*big.Int,
	key string,
	amount *big.Int,
) error {
	if dict == nil || *dict == nil || amount == nil {
		return ErrInvalidUpsertArgument
	}
	d := *dict
	if _, exists := d[key]; !exists {
		d[key] = new(big.Int).Set(amount)
	} else {
		d[key] = new(big.Int).Add(d[key], amount)
	}
	return nil
}
