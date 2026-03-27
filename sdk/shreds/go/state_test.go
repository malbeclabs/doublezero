package shreds

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unsafe"
)

func TestStructSizes(t *testing.T) {
	tests := []struct {
		name     string
		size     uintptr
		expected uintptr
	}{
		{"ProgramConfig", unsafe.Sizeof(ProgramConfig{}), 248},
		{"ExecutionController", unsafe.Sizeof(ExecutionController{}), 144},
		{"ClientSeat", unsafe.Sizeof(ClientSeat{}), 232},
		{"PaymentEscrow", unsafe.Sizeof(PaymentEscrow{}), 136},
		{"ShredDistribution", unsafe.Sizeof(ShredDistribution{}), 392},
		{"ValidatorClientRewards", unsafe.Sizeof(ValidatorClientRewards{}), 168},
		{"InstantSeatAllocationRequest", unsafe.Sizeof(InstantSeatAllocationRequest{}), 48},
		{"WithdrawSeatRequest", unsafe.Sizeof(WithdrawSeatRequest{}), 36},
		{"MetroPrice", unsafe.Sizeof(MetroPrice{}), 72},
		{"MetroPriceEntry", unsafe.Sizeof(MetroPriceEntry{}), 80},
		{"MetroPriceRingBuffer", unsafe.Sizeof(MetroPriceRingBuffer{}), 2568},
		{"MetroHistory", unsafe.Sizeof(MetroHistory{}), 2744},
		{"DeviceSubscription", unsafe.Sizeof(DeviceSubscription{}), 72},
		{"DeviceSubscriptionEntry", unsafe.Sizeof(DeviceSubscriptionEntry{}), 80},
		{"DeviceSubscriptionRingBuffer", unsafe.Sizeof(DeviceSubscriptionRingBuffer{}), 2568},
		{"DeviceHistory", unsafe.Sizeof(DeviceHistory{}), 2776},
		{"ValidatorClientRewardsProportion", unsafe.Sizeof(ValidatorClientRewardsProportion{}), 4},
		{"ValidatorClientRewardsConfig", unsafe.Sizeof(ValidatorClientRewardsConfig{}), 136},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.size != tt.expected {
				t.Errorf("sizeof(%s) = %d, want %d", tt.name, tt.size, tt.expected)
			}
		})
	}
}

func TestExecutionControllerDeserialization(t *testing.T) {
	data := make([]byte, unsafe.Sizeof(ExecutionController{}))
	data[0] = 2                                    // Phase = OpenForRequests
	data[1] = 42                                   // BumpSeed
	binary.LittleEndian.PutUint16(data[4:], 5)     // TotalMetros
	binary.LittleEndian.PutUint16(data[6:], 10)    // TotalEnabledDevices
	binary.LittleEndian.PutUint32(data[8:], 100)   // TotalClientSeats
	binary.LittleEndian.PutUint64(data[24:], 42)   // CurrentSubscriptionEpoch
	binary.LittleEndian.PutUint64(data[136:], 999) // NextSeatFundingIndex

	var ec ExecutionController
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &ec); err != nil {
		t.Fatalf("deserializing: %v", err)
	}
	if ec.GetPhase() != ExecutionPhaseOpenForRequests {
		t.Errorf("Phase = %d, want %d", ec.Phase, ExecutionPhaseOpenForRequests)
	}
	if ec.BumpSeed != 42 {
		t.Errorf("BumpSeed = %d, want 42", ec.BumpSeed)
	}
	if ec.TotalMetros != 5 {
		t.Errorf("TotalMetros = %d, want 5", ec.TotalMetros)
	}
	if ec.TotalEnabledDevices != 10 {
		t.Errorf("TotalEnabledDevices = %d, want 10", ec.TotalEnabledDevices)
	}
	if ec.TotalClientSeats != 100 {
		t.Errorf("TotalClientSeats = %d, want 100", ec.TotalClientSeats)
	}
	if ec.CurrentSubscriptionEpoch != 42 {
		t.Errorf("CurrentSubscriptionEpoch = %d, want 42", ec.CurrentSubscriptionEpoch)
	}
	if ec.NextSeatFundingIndex != 999 {
		t.Errorf("NextSeatFundingIndex = %d, want 999", ec.NextSeatFundingIndex)
	}
}

func TestClientSeatDeserialization(t *testing.T) {
	data := make([]byte, unsafe.Sizeof(ClientSeat{}))
	// Set device_key to known pattern.
	for i := range 32 {
		data[i] = byte(i + 1)
	}
	binary.LittleEndian.PutUint32(data[32:], 0x0A000001) // ClientIPBits = 10.0.0.1
	binary.LittleEndian.PutUint16(data[38:], 7)          // TenureEpochs
	binary.LittleEndian.PutUint64(data[40:], 1)          // Flags = HAS_PRICE_OVERRIDE
	binary.LittleEndian.PutUint32(data[136:], 3)         // EscrowCount
	binary.LittleEndian.PutUint16(data[140:], 500)       // OverrideUSDCPriceDollars

	var seat ClientSeat
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &seat); err != nil {
		t.Fatalf("deserializing: %v", err)
	}
	if seat.DeviceKey[0] != 1 || seat.DeviceKey[31] != 32 {
		t.Error("DeviceKey not deserialized correctly")
	}
	if seat.ClientIPBits != 0x0A000001 {
		t.Errorf("ClientIPBits = %x, want 0A000001", seat.ClientIPBits)
	}
	if seat.TenureEpochs != 7 {
		t.Errorf("TenureEpochs = %d, want 7", seat.TenureEpochs)
	}
	if !seat.HasPriceOverride() {
		t.Error("expected HasPriceOverride = true")
	}
	if seat.EscrowCount != 3 {
		t.Errorf("EscrowCount = %d, want 3", seat.EscrowCount)
	}
	if seat.OverrideUSDCPriceDollars != 500 {
		t.Errorf("OverrideUSDCPriceDollars = %d, want 500", seat.OverrideUSDCPriceDollars)
	}
}

func TestPaymentEscrowDeserialization(t *testing.T) {
	data := make([]byte, unsafe.Sizeof(PaymentEscrow{}))
	for i := range 32 {
		data[i] = byte(i + 1) // ClientSeatKey
	}
	for i := range 32 {
		data[32+i] = byte(i + 33) // WithdrawAuthorityKey
	}
	binary.LittleEndian.PutUint64(data[64:], 1_000_000) // USDCBalance = 1 USDC

	var escrow PaymentEscrow
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &escrow); err != nil {
		t.Fatalf("deserializing: %v", err)
	}
	if escrow.ClientSeatKey[0] != 1 {
		t.Error("ClientSeatKey not deserialized correctly")
	}
	if escrow.WithdrawAuthorityKey[0] != 33 {
		t.Error("WithdrawAuthorityKey not deserialized correctly")
	}
	if escrow.USDCBalance != 1_000_000 {
		t.Errorf("USDCBalance = %d, want 1000000", escrow.USDCBalance)
	}
}

func TestShredDistributionDeserialization(t *testing.T) {
	data := make([]byte, unsafe.Sizeof(ShredDistribution{}))
	binary.LittleEndian.PutUint64(data[0:], 42)         // SubscriptionEpoch
	binary.LittleEndian.PutUint64(data[16:], 100)       // AssociatedDZEpoch
	binary.LittleEndian.PutUint16(data[28:], 5)         // DeviceCount
	binary.LittleEndian.PutUint16(data[30:], 20)        // ClientSeatCount
	binary.LittleEndian.PutUint64(data[72:], 5_000_000) // CollectedUSDCPayments

	var dist ShredDistribution
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &dist); err != nil {
		t.Fatalf("deserializing: %v", err)
	}
	if dist.SubscriptionEpoch != 42 {
		t.Errorf("SubscriptionEpoch = %d, want 42", dist.SubscriptionEpoch)
	}
	if dist.AssociatedDZEpoch != 100 {
		t.Errorf("AssociatedDZEpoch = %d, want 100", dist.AssociatedDZEpoch)
	}
	if dist.DeviceCount != 5 {
		t.Errorf("DeviceCount = %d, want 5", dist.DeviceCount)
	}
	if dist.ClientSeatCount != 20 {
		t.Errorf("ClientSeatCount = %d, want 20", dist.ClientSeatCount)
	}
	if dist.CollectedUSDCPayments != 5_000_000 {
		t.Errorf("CollectedUSDCPayments = %d, want 5000000", dist.CollectedUSDCPayments)
	}
}

func TestValidatorClientRewardsDeserialization(t *testing.T) {
	data := make([]byte, unsafe.Sizeof(ValidatorClientRewards{}))
	binary.LittleEndian.PutUint16(data[0:], 42) // ClientID
	// Manager key at offset 8
	for i := range 32 {
		data[8+i] = byte(i + 1)
	}
	// Short description at offset 40
	desc := "Test Client"
	copy(data[40:], desc)

	var vcr ValidatorClientRewards
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &vcr); err != nil {
		t.Fatalf("deserializing: %v", err)
	}
	if vcr.ClientID != 42 {
		t.Errorf("ClientID = %d, want 42", vcr.ClientID)
	}
	if vcr.ManagerKey[0] != 1 {
		t.Error("ManagerKey not deserialized correctly")
	}
	if vcr.ShortDescription() != desc {
		t.Errorf("ShortDescription() = %q, want %q", vcr.ShortDescription(), desc)
	}
}

func TestDeserializeAccountWithDiscriminator(t *testing.T) {
	// Build a full account blob: discriminator + PaymentEscrow body.
	body := make([]byte, unsafe.Sizeof(PaymentEscrow{}))
	binary.LittleEndian.PutUint64(body[64:], 42) // USDCBalance

	data := make([]byte, discriminatorSize+len(body))
	copy(data[:8], DiscriminatorPaymentEscrow[:])
	copy(data[8:], body)

	escrow, err := deserializeAccount[PaymentEscrow](data, DiscriminatorPaymentEscrow)
	if err != nil {
		t.Fatalf("deserializeAccount: %v", err)
	}
	if escrow.USDCBalance != 42 {
		t.Errorf("USDCBalance = %d, want 42", escrow.USDCBalance)
	}

	// Wrong discriminator should fail.
	_, err = deserializeAccount[PaymentEscrow](data, DiscriminatorClientSeat)
	if err == nil {
		t.Fatal("expected error for wrong discriminator")
	}
}

func TestMetroHistoryDeserialization(t *testing.T) {
	data := make([]byte, unsafe.Sizeof(MetroHistory{}))
	// Set exchange key
	for i := range 32 {
		data[i] = byte(i + 1)
	}
	// Flags with IS_CURRENT_PRICE_FINALIZED (bit 1)
	binary.LittleEndian.PutUint64(data[32:], 1<<1)
	// TotalInitializedDevices
	binary.LittleEndian.PutUint16(data[40:], 3)

	// RingBuffer starts at offset 176 (32 + 8 + 2 + 6 + 128)
	ringOffset := 176
	data[ringOffset] = 1   // CurrentIndex
	data[ringOffset+1] = 2 // TotalCount

	// First entry at ringOffset + 8
	entryOffset := ringOffset + 8
	binary.LittleEndian.PutUint64(data[entryOffset:], 100)    // Epoch
	binary.LittleEndian.PutUint16(data[entryOffset+8:], 5000) // USDCPriceDollars

	var mh MetroHistory
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &mh); err != nil {
		t.Fatalf("deserializing: %v", err)
	}
	if mh.ExchangeKey[0] != 1 {
		t.Error("ExchangeKey not deserialized correctly")
	}
	if !mh.IsCurrentPriceFinalized() {
		t.Error("expected IsCurrentPriceFinalized = true")
	}
	if mh.TotalInitializedDevices != 3 {
		t.Errorf("TotalInitializedDevices = %d, want 3", mh.TotalInitializedDevices)
	}
	if mh.Prices.CurrentIndex != 1 {
		t.Errorf("Prices.CurrentIndex = %d, want 1", mh.Prices.CurrentIndex)
	}
	if mh.Prices.TotalCount != 2 {
		t.Errorf("Prices.TotalCount = %d, want 2", mh.Prices.TotalCount)
	}
	if mh.Prices.Entries[0].Epoch != 100 {
		t.Errorf("first entry epoch = %d, want 100", mh.Prices.Entries[0].Epoch)
	}
	if mh.Prices.Entries[0].Price.USDCPriceDollars != 5000 {
		t.Errorf("first entry price = %d, want 5000", mh.Prices.Entries[0].Price.USDCPriceDollars)
	}
}

func TestDeviceHistoryDeserialization(t *testing.T) {
	data := make([]byte, unsafe.Sizeof(DeviceHistory{}))
	// Device key
	for i := range 32 {
		data[i] = byte(i + 1)
	}
	// Flags with IS_ENABLED (bit 1) and HAS_SETTLED_SEATS (bit 2)
	binary.LittleEndian.PutUint64(data[32:], (1<<1)|(1<<2))
	data[40] = 255 // BumpSeed
	data[41] = 254 // USDCTokenPDABumpSeed
	// ActiveGrantedSeats at offset 80
	binary.LittleEndian.PutUint16(data[80:], 7)
	// ActiveTotalAvailableSeats at offset 82
	binary.LittleEndian.PutUint16(data[82:], 10)

	// RingBuffer starts at offset 208 (32 + 8 + 1 + 1 + 6 + 32 + 2 + 2 + 28 + 96)
	ringOffset := 208
	data[ringOffset] = 0   // CurrentIndex
	data[ringOffset+1] = 1 // TotalCount

	// First entry at ringOffset + 8
	entryOffset := ringOffset + 8
	binary.LittleEndian.PutUint64(data[entryOffset:], 50) // Epoch
	// DeviceSubscription starts at entryOffset + 8
	subOffset := entryOffset + 8
	neg100 := int16(-100)
	binary.LittleEndian.PutUint16(data[subOffset:], uint16(neg100)) // USDCMetroPremiumDollars = -100
	binary.LittleEndian.PutUint16(data[subOffset+2:], 20)           // RequestedSeatCount
	binary.LittleEndian.PutUint16(data[subOffset+4:], 15)           // TotalAvailableSeats
	binary.LittleEndian.PutUint16(data[subOffset+6:], 10)           // GrantedSeatCount

	var dh DeviceHistory
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &dh); err != nil {
		t.Fatalf("deserializing: %v", err)
	}
	if !dh.IsEnabled() {
		t.Error("expected IsEnabled = true")
	}
	if !dh.HasSettledSeats() {
		t.Error("expected HasSettledSeats = true")
	}
	if dh.ActiveGrantedSeats != 7 {
		t.Errorf("ActiveGrantedSeats = %d, want 7", dh.ActiveGrantedSeats)
	}
	if dh.ActiveTotalAvailableSeats != 10 {
		t.Errorf("ActiveTotalAvailableSeats = %d, want 10", dh.ActiveTotalAvailableSeats)
	}

	sub := dh.Subscriptions.Entries[0].Subscription
	if sub.USDCMetroPremiumDollars != -100 {
		t.Errorf("USDCMetroPremiumDollars = %d, want -100", sub.USDCMetroPremiumDollars)
	}
	if sub.RequestedSeatCount != 20 {
		t.Errorf("RequestedSeatCount = %d, want 20", sub.RequestedSeatCount)
	}
	if sub.TotalAvailableSeats != 15 {
		t.Errorf("TotalAvailableSeats = %d, want 15", sub.TotalAvailableSeats)
	}
	if sub.GrantedSeatCount != 10 {
		t.Errorf("GrantedSeatCount = %d, want 10", sub.GrantedSeatCount)
	}
}

func TestExecutionPhaseString(t *testing.T) {
	tests := []struct {
		phase ExecutionPhase
		want  string
	}{
		{ExecutionPhaseClosedForRequests, "closed for requests"},
		{ExecutionPhaseUpdatingPrices, "updating prices"},
		{ExecutionPhaseOpenForRequests, "open for requests"},
		{ExecutionPhase(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.phase.String(); got != tt.want {
			t.Errorf("ExecutionPhase(%d).String() = %q, want %q", tt.phase, got, tt.want)
		}
	}
}
