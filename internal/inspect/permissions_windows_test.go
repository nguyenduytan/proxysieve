//go:build windows

package inspect

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestSameACLAllowsCanonicalOrderOnlyForExpectedPrincipals(t *testing.T) {
	expectedDescriptor, err := privateSecurityDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	expected, _, err := expectedDescriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	reorderedDescriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	reordered, _, err := reorderedDescriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if !sameACL(reordered, expected) {
		t.Fatal("equivalent reordered ACL rejected")
	}
	unsafeDescriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;" + user.User.Sid.String() + ")(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	unsafeACL, _, err := unsafeDescriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if sameACL(unsafeACL, expected) {
		t.Fatal("ACL with an extra principal accepted")
	}
}
