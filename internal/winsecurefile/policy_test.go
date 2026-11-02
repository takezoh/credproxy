package winsecurefile

import "testing"

const userSID = "S-1-5-21-100-200-300-1001"

func TestSecretPolicyRequiresCurrentOwnerAndPrivateTrustees(t *testing.T) {
	entries := []accessACE{
		{kind: aceAllowed, sid: userSID, mask: genericAll},
		{kind: aceAllowed, sid: systemSID, mask: genericAll},
		{kind: aceAllowed, sid: ownerRightsSID, mask: genericAll},
		{kind: aceDenied, sid: "S-1-1-0", mask: genericAll},
	}
	if err := validateSecretPolicy(userSID, userSID, entries); err != nil {
		t.Fatalf("valid secret ACL rejected: %v", err)
	}
	if err := validateSecretPolicy(systemSID, userSID, entries); err == nil {
		t.Fatal("accepted a secret file owned by SYSTEM")
	}
	entries = append(entries, accessACE{kind: aceAllowed, sid: "S-1-1-0", mask: fileReadData})
	if err := validateSecretPolicy(userSID, userSID, entries); err == nil {
		t.Fatal("accepted a secret ACL granting access to Everyone")
	}
}

func TestManagedImagePolicyAcceptsMachineManagedReadExecuteImage(t *testing.T) {
	entries := []accessACE{
		{kind: aceAllowed, sid: systemSID, mask: genericAll},
		{kind: aceAllowed, sid: localAdministratorsSID, mask: genericAll},
		{kind: aceAllowed, sid: trustedInstallerSID, mask: genericAll},
		{kind: aceAllowed, sid: userSID, mask: genericRead | genericExecute},
		{kind: aceAllowed, sid: "S-1-1-0", mask: genericRead | genericExecute},
		{kind: aceAllowed, sid: "S-1-1-0", mask: genericAll, inheritOnly: true},
		{kind: aceAllowed, sid: ownerRightsSID, mask: genericAll},
	}
	if err := validateManagedImagePolicy(systemSID, userSID, entries); err != nil {
		t.Fatalf("protected machine-managed image rejected: %v", err)
	}
}

func TestManagedImagePolicyRejectsUntrustedMutationRights(t *testing.T) {
	for _, mask := range []uint32{
		fileReadData | 0x00000002, // FILE_WRITE_DATA
		fileReadData | 0x00010000, // DELETE
		fileReadData | 0x00040000, // WRITE_DAC
		genericWrite,
		genericAll,
	} {
		entries := []accessACE{{kind: aceAllowed, sid: "S-1-1-0", mask: mask}}
		if err := validateManagedImagePolicy(systemSID, userSID, entries); err == nil {
			t.Errorf("accepted untrusted image access mask %#x", mask)
		}
	}
}

func TestManagedImagePolicyRequiresTrustedOwnerAndKnownACEForms(t *testing.T) {
	entries := []accessACE{{kind: aceAllowed, sid: userSID, mask: fileReadData}}
	if err := validateManagedImagePolicy(systemSID, "", entries); err == nil {
		t.Fatal("accepted a managed image without the current user SID")
	}
	if err := validateManagedImagePolicy("S-1-1-0", userSID, entries); err == nil {
		t.Fatal("accepted an image owned by an untrusted principal")
	}
	if err := validateManagedImagePolicy(systemSID, userSID, []accessACE{{kind: aceUnsupported}}); err == nil {
		t.Fatal("accepted an unsupported image DACL ACE")
	}
}
