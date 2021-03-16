// Copyright 2017 The Celo Authors
// This file is part of the celo library.
//
// The celo library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The celo library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the celo library. If not, see <http://www.gnu.org/licenses/>.
package caller

import (
	"math/big"
	"reflect"
	"time"

	"github.com/celo-org/celo-blockchain/accounts/abi"
	"github.com/celo-org/celo-blockchain/common"
	"github.com/celo-org/celo-blockchain/common/hexutil"
	"github.com/celo-org/celo-blockchain/contract_comm/errors"
	"github.com/celo-org/celo-blockchain/core/types"
	"github.com/celo-org/celo-blockchain/core/vm"
	"github.com/celo-org/celo-blockchain/log"
	"github.com/celo-org/celo-blockchain/metrics"
)

// Implementation
// An EVM handler to make calls to smart contracts from within geth
type ContractCaller struct {
	chain vm.ChainContext
}

func New(chain vm.ChainContext) (ContractCaller, error) {
	return ContractCaller{chain: chain}, nil
}

// Cannot make this const, but serves as an empty message
var emptyMessage = types.NewMessage(common.HexToAddress("0x0"), nil, 0, common.Big0, 0, common.Big0, nil, nil, common.Big0, []byte{}, false)

func (c ContractCaller) MakeStaticCall(registryId [32]byte, abi abi.ABI, funcName string, args []interface{}, returnObj interface{}, gas uint64, header *types.Header, state vm.StateDB) (uint64, error) {
	return c.makeCallWithContractId(registryId, abi, funcName, args, returnObj, gas, nil, header, state, true)
}

func (c ContractCaller) MakeCall(registryId [32]byte, abi abi.ABI, funcName string, args []interface{}, returnObj interface{}, gas uint64, value *big.Int, header *types.Header, state vm.StateDB, finaliseState bool) (uint64, error) {
	gasLeft, err := c.makeCallWithContractId(registryId, abi, funcName, args, returnObj, gas, value, header, state, false)
	if err == nil && finaliseState {
		state.Finalise(true)
	}
	return gasLeft, err
}

func (c ContractCaller) MakeStaticCallWithAddress(scAddress common.Address, abi abi.ABI, funcName string, args []interface{}, returnObj interface{}, gas uint64, header *types.Header, state vm.StateDB) (uint64, error) {
	return c.makeCallFromSystem(scAddress, abi, funcName, args, returnObj, gas, nil, header, state, true)
}

func (c ContractCaller) GetRegisteredAddress(registryId [32]byte, header *types.Header, state vm.StateDB) (*common.Address, error) {
	vmevm, err := c.createEVM(header, state)
	if err != nil {
		return nil, err
	}
	return vm.GetRegisteredAddressWithEvm(registryId, vmevm)
}

func (c ContractCaller) createEVM(header *types.Header, state vm.StateDB) (*vm.EVM, error) {
	// Normally, when making an evm call, we should use the current block's state.  However,
	// there are times (e.g. retrieving the set of validators when an epoch ends) that we need
	// to call the evm using the currently mined block.  In that case, the header and state params
	// will be non nil.
	if c.chain == nil {
		return nil, errors.ErrNoInternalEvmHandlerSingleton
	}

	if header == nil {
		header = c.chain.CurrentHeader()
	}

	if state == nil || reflect.ValueOf(state).IsNil() {
		var err error
		state, err = c.chain.State()
		if err != nil {
			log.Error("Error in retrieving the state from the blockchain", "err", err)
			return nil, err
		}
	}

	// The EVM Context requires a msg, but the actual field values don't really matter for this case.
	// Putting in zero values.
	context := vm.NewEVMContext(emptyMessage, header, c.chain, nil)
	evm := vm.NewEVM(context, state, c.chain.Config(), *c.chain.GetVMConfig())

	return evm, nil
}

func (c ContractCaller) makeCallFromSystem(scAddress common.Address, abi abi.ABI, funcName string, args []interface{}, returnObj interface{}, gas uint64, value *big.Int, header *types.Header, state vm.StateDB, static bool) (uint64, error) {
	// Record a metrics data point about execution time.
	timer := metrics.GetOrRegisterTimer("contract_comm/systemcall/"+funcName, nil)
	start := time.Now()
	defer timer.UpdateSince(start)

	vmevm, err := c.createEVM(header, state)
	if err != nil {
		return 0, err
	}

	var gasLeft uint64

	if static {
		gasLeft, err = vmevm.StaticCallFromSystem(scAddress, abi, funcName, args, returnObj, gas)
	} else {
		gasLeft, err = vmevm.CallFromSystem(scAddress, abi, funcName, args, returnObj, gas, value)
	}
	if err != nil {
		log.Error("Error when invoking evm function", "err", err, "funcName", funcName, "static", static, "address", scAddress, "args", args, "gas", gas, "gasLeft", gasLeft, "value", value)
		return gasLeft, err
	}

	return gasLeft, nil
}

func (c ContractCaller) makeCallWithContractId(registryId [32]byte, abi abi.ABI, funcName string, args []interface{}, returnObj interface{}, gas uint64, value *big.Int, header *types.Header, state vm.StateDB, static bool) (uint64, error) {
	scAddress, err := c.GetRegisteredAddress(registryId, header, state)

	if err != nil {
		if err == errors.ErrSmartContractNotDeployed {
			log.Debug("Contract not yet registered", "function", funcName, "registryId", hexutil.Encode(registryId[:]))
			return 0, err
		} else if err == errors.ErrRegistryContractNotDeployed {
			log.Debug("Registry contract not yet deployed", "function", funcName, "registryId", hexutil.Encode(registryId[:]))
			return 0, err
		} else {
			log.Error("Error in getting registered address", "function", funcName, "registryId", hexutil.Encode(registryId[:]), "err", err)
			return 0, err
		}
	}

	gasLeft, err := c.makeCallFromSystem(*scAddress, abi, funcName, args, returnObj, gas, value, header, state, static)
	if err != nil {
		log.Error("Error in executing function on registered contract", "function", funcName, "registryId", hexutil.Encode(registryId[:]), "err", err)
	}
	return gasLeft, err
}
