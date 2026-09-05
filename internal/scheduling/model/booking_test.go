package model

import "testing"

func bptr(s string) *string { return &s }

func TestValidateCustomerRequiresANonBlankName(t *testing.T) {
	for _, name := range []string{"", "   ", "\t"} {
		_, err := ValidateCustomer(Customer{Name: name})
		assertValidationFailed(t, err, "ValidateCustomer(blank name)")
	}
}

func TestValidateCustomerTrimsNameAndKeepsOptionalContact(t *testing.T) {
	got, err := ValidateCustomer(Customer{Name: "  Jane Doe  ", Phone: bptr(" +234 800 111 2222 "), Email: bptr("  jane@example.com ")})
	if err != nil {
		t.Fatalf("ValidateCustomer() error = %v", err)
	}
	if got.Name != "Jane Doe" {
		t.Fatalf("name = %q, want trimmed", got.Name)
	}
	if got.Phone == nil || *got.Phone != "+234 800 111 2222" {
		t.Fatalf("phone = %v, want trimmed free text", got.Phone)
	}
	if got.Email == nil || *got.Email != "jane@example.com" {
		t.Fatalf("email = %v", got.Email)
	}
}

func TestValidateCustomerCollapsesBlankOptionalContactToNil(t *testing.T) {
	got, err := ValidateCustomer(Customer{Name: "Jane", Phone: bptr("   "), Email: bptr("")})
	if err != nil {
		t.Fatalf("ValidateCustomer() error = %v", err)
	}
	if got.Phone != nil || got.Email != nil {
		t.Fatalf("blank optional contact should collapse to nil: phone=%v email=%v", got.Phone, got.Email)
	}
}

func TestValidateCustomerRejectsAMalformedEmail(t *testing.T) {
	_, err := ValidateCustomer(Customer{Name: "Jane", Email: bptr("not-an-email")})
	assertValidationFailed(t, err, "ValidateCustomer(malformed email)")
}

func TestValidateCustomerBoundsLengths(t *testing.T) {
	long := make([]byte, MaxCustomerNameLength+1)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := ValidateCustomer(Customer{Name: string(long)}); err == nil {
		t.Fatal("an over-long name was accepted")
	}
}
