package typesutil

import (
	"fmt"
	"strings"

	"github.com/uplo-tech/uplo/crypto"
	"github.com/uplo-tech/uplo/types"
)

// SprintTxnWithObjectIDs creates a string representing this Transaction in human-readable form with all
// object IDs included to allow for easy dependency matching (by humans) in
// debug-logs.
func SprintTxnWithObjectIDs(t types.Transaction) string {
	var str strings.Builder
	txIDString := crypto.Hash(t.ID()).String()
	fmt.Fprintf(&str, "\nTransaction ID: %s", txIDString)

	if len(t.UplocoinInputs) != 0 {
		fmt.Fprintf(&str, "\nUplocoinInputs:\n")
		for i, input := range t.UplocoinInputs {
			parentIDString := crypto.Hash(input.ParentID).String()
			fmt.Fprintf(&str, "\t%d: %s\n", i, parentIDString)
		}
	}
	if len(t.UplocoinOutputs) != 0 {
		fmt.Fprintf(&str, "UplocoinOutputs:\n")
		for i := range t.UplocoinOutputs {
			oidString := crypto.Hash(t.UplocoinOutputID(uint64(i))).String()
			fmt.Fprintf(&str, "\t%d: %s\n", i, oidString)
		}
	}
	if len(t.FileContracts) != 0 {
		fmt.Fprintf(&str, "FileContracts:\n")
		for i := range t.FileContracts {
			fcIDString := crypto.Hash(t.FileContractID(uint64(i))).String()
			fmt.Fprintf(&str, "\t%d: %s\n", i, fcIDString)
		}
	}
	if len(t.FileContractRevisions) != 0 {
		fmt.Fprintf(&str, "FileContractRevisions:\n")
		for _, fcr := range t.FileContractRevisions {
			parentIDString := crypto.Hash(fcr.ParentID).String()
			fmt.Fprintf(&str, "\t%d, %s\n", fcr.NewRevisionNumber, parentIDString)
		}
	}
	if len(t.StorageProofs) != 0 {
		fmt.Fprintf(&str, "StorageProofs:\n")
		for _, sp := range t.StorageProofs {
			parentIDString := crypto.Hash(sp.ParentID).String()
			fmt.Fprintf(&str, "\t%s\n", parentIDString)
		}
	}
	if len(t.UplofundInputs) != 0 {
		fmt.Fprintf(&str, "UplofundInputs:\n")
		for i, input := range t.UplofundInputs {
			parentIDString := crypto.Hash(input.ParentID).String()
			fmt.Fprintf(&str, "\t%d: %s\n", i, parentIDString)
		}
	}
	if len(t.UplofundOutputs) != 0 {
		fmt.Fprintf(&str, "UplofundOutputs:\n")
		for i := range t.UplofundOutputs {
			oidString := crypto.Hash(t.UplofundOutputID(uint64(i))).String()
			fmt.Fprintf(&str, "\t%d: %s\n", i, oidString)
		}
	}
	return str.String()
}
