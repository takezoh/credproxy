package winsecurefile

import (
	"fmt"
	"strings"
)

type aceKind uint8

const (
	aceUnsupported aceKind = iota
	aceAllowed
	aceDenied
)

type accessACE struct {
	kind        aceKind
	sid         string
	mask        uint32
	inheritOnly bool
}

const (
	systemSID              = "S-1-5-18"
	localAdministratorsSID = "S-1-5-32-544"
	trustedInstallerSID    = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
	ownerRightsSID         = "S-1-3-4"

	genericAll     = uint32(0x10000000)
	genericExecute = uint32(0x20000000)
	genericWrite   = uint32(0x40000000)
	genericRead    = uint32(0x80000000)

	fileReadData       = uint32(0x00000001)
	fileExecute        = uint32(0x00000020)
	fileReadEA         = uint32(0x00000008)
	fileReadAttributes = uint32(0x00000080)
	readControl        = uint32(0x00020000)
	synchronize        = uint32(0x00100000)

	fileGenericRead = fileReadData | fileReadEA | fileReadAttributes | readControl | synchronize
	fileGenericExec = fileExecute | fileReadAttributes | readControl | synchronize
	imageReadExec   = fileGenericRead | fileGenericExec
)

func validateSecretPolicy(ownerSID, currentSID string, entries []accessACE) error {
	if !sameSID(ownerSID, currentSID) {
		return fmt.Errorf("protected secret owner SID mismatch")
	}
	if len(entries) == 0 {
		return fmt.Errorf("protected secret has no restrictive DACL")
	}
	for _, entry := range entries {
		if entry.inheritOnly {
			continue
		}
		switch entry.kind {
		case aceDenied:
			continue
		case aceAllowed:
			if !sameSID(entry.sid, currentSID) && !sameSID(entry.sid, systemSID) && !sameSID(entry.sid, ownerRightsSID) {
				return fmt.Errorf("protected secret DACL grants access outside owner and SYSTEM")
			}
		default:
			return fmt.Errorf("protected secret DACL contains an unsupported ACE")
		}
	}
	return nil
}

func validateManagedImagePolicy(ownerSID, currentSID string, entries []accessACE) error {
	if currentSID == "" {
		return fmt.Errorf("current user SID is unavailable for managed image admission")
	}
	if !trustedImageSID(ownerSID, currentSID) {
		return fmt.Errorf("managed image owner is not trusted")
	}
	if len(entries) == 0 {
		return fmt.Errorf("managed image has no restrictive DACL")
	}
	for _, entry := range entries {
		if entry.inheritOnly {
			continue
		}
		switch entry.kind {
		case aceDenied:
			continue
		case aceAllowed:
			if trustedImageSID(entry.sid, currentSID) || sameSID(entry.sid, ownerRightsSID) {
				continue
			}
			if !readExecuteOnly(entry.mask) {
				return fmt.Errorf("managed image DACL grants untrusted write or control access")
			}
		default:
			return fmt.Errorf("managed image DACL contains an unsupported ACE")
		}
	}
	return nil
}

func trustedImageSID(sid, currentSID string) bool {
	return sameSID(sid, currentSID) || sameSID(sid, systemSID) ||
		sameSID(sid, localAdministratorsSID) || sameSID(sid, trustedInstallerSID)
}

func sameSID(left, right string) bool {
	return left != "" && right != "" && strings.EqualFold(left, right)
}

func readExecuteOnly(mask uint32) bool {
	if mask&(genericWrite|genericAll) != 0 {
		return false
	}
	allowed := mask &^ (genericRead | genericExecute)
	if mask&genericRead != 0 {
		allowed |= fileGenericRead
	}
	if mask&genericExecute != 0 {
		allowed |= fileGenericExec
	}
	return allowed&^imageReadExec == 0
}
