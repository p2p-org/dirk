// Copyright © 2021 Attestant Limited.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package standard_test

import (
	context "context"
	"fmt"
	"testing"

	"github.com/attestantio/dirk/core"
	"github.com/attestantio/dirk/rules"
	mockrules "github.com/attestantio/dirk/rules/mock"
	"github.com/attestantio/dirk/services/checker"
	mockchecker "github.com/attestantio/dirk/services/checker/mock"
	memfetcher "github.com/attestantio/dirk/services/fetcher/mem"
	syncmaplocker "github.com/attestantio/dirk/services/locker/syncmap"
	"github.com/attestantio/dirk/services/ruler/golang"
	standardsigner "github.com/attestantio/dirk/services/signer/standard"
	localunlocker "github.com/attestantio/dirk/services/unlocker/local"
	"github.com/attestantio/dirk/testing/logger"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	keystorev4 "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"
	hd "github.com/wealdtech/go-eth2-wallet-hd/v2"
	scratch "github.com/wealdtech/go-eth2-wallet-store-scratch"
	e2wtypes "github.com/wealdtech/go-eth2-wallet-types/v2"
)

func TestMultisign(t *testing.T) {
	ctx := context.Background()

	store := scratch.New()
	encryptor := keystorev4.New()
	seed := []byte{
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
		0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
	}

	wallet, err := hd.CreateWallet(ctx, "Test wallet", []byte("secret"), store, encryptor, seed)
	require.NoError(t, err)
	require.NoError(t, wallet.(e2wtypes.WalletLocker).Unlock(ctx, []byte("secret")))

	accountNames := []string{
		"Test account 1",
		"Test account 2",
	}
	pubKeys := make(map[string][]byte)
	for _, accountName := range accountNames {
		passphrase := []byte(fmt.Sprintf("%s passphrase", accountName))
		account, err := wallet.(e2wtypes.WalletAccountCreator).CreateAccount(ctx, accountName, passphrase)
		require.NoError(t, err)
		pubKeys[accountName] = account.PublicKey().Marshal()
		require.NoError(t, account.(e2wtypes.AccountLocker).Unlock(context.Background(), passphrase))
	}
	require.NoError(t, wallet.(e2wtypes.WalletLocker).Lock(ctx))

	lockerSvc, err := syncmaplocker.New(ctx)
	require.NoError(t, err)

	fetcherSvc, err := memfetcher.New(ctx,
		memfetcher.WithStores([]e2wtypes.Store{store}))
	require.NoError(t, err)

	unlockerSvc, err := localunlocker.New(context.Background(),
		localunlocker.WithAccountPassphrases([]string{"Test account 1 passphrase"}))
	require.NoError(t, err)

	tests := []struct {
		name         string
		credentials  *checker.Credentials
		accountNames []string
		pubKeys      [][]byte
		data         []*rules.SignData
		res          []core.Result
		logEntry     string
	}{
		{
			name:     "Nil",
			res:      []core.Result{core.ResultDenied},
			logEntry: "Request empty",
		},
		{
			name:        "DataNil",
			credentials: &checker.Credentials{Client: "client1"},
			res:         []core.Result{core.ResultDenied},
			logEntry:    "Request empty",
		},
		{
			name:        "DataEmpty",
			credentials: &checker.Credentials{Client: "client1"},
			data:        []*rules.SignData{nil},
			res:         []core.Result{core.ResultDenied},
			logEntry:    "Check failed",
		},
		{
			name:     "CredentialsNil",
			data:     []*rules.SignData{{}},
			res:      []core.Result{core.ResultDenied},
			logEntry: "No credentials supplied",
		},
		{
			name:        "FailPreCheck",
			credentials: &checker.Credentials{Client: "client1"},
			data:        []*rules.SignData{},
			res:         []core.Result{core.ResultDenied},
			logEntry:    "Request empty",
		},
		{
			name:        "DomainMissing",
			credentials: &checker.Credentials{Client: "client1"},
			data: []*rules.SignData{
				{
					Data: []byte{
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
					},
				},
			},
			accountNames: []string{"Test wallet/Test account 1"},
			res:          []core.Result{core.ResultDenied},
			logEntry:     "Check failed",
		},
		{
			name:        "DataMissing",
			credentials: &checker.Credentials{Client: "client1"},
			data: []*rules.SignData{
				{
					Domain: []byte{
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
					},
				},
			},
			accountNames: []string{"Test wallet/Test account 1"},
			res:          []core.Result{core.ResultDenied},
			logEntry:     "Check failed",
		},
		{
			name:        "GoodName",
			credentials: &checker.Credentials{Client: "client1"},
			data: []*rules.SignData{
				{
					Data: []byte{
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
					},
					Domain: []byte{
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
					},
				},
			},
			accountNames: []string{"Test wallet/Test account 1"},
			res:          []core.Result{core.ResultSucceeded},
		},
		{
			name:        "GoodPubKey",
			credentials: &checker.Credentials{Client: "client1"},
			data: []*rules.SignData{
				{
					Data: []byte{
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
					},
					Domain: []byte{
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
					},
				},
			},
			pubKeys: [][]byte{pubKeys["Test account 1"]},
			res:     []core.Result{core.ResultSucceeded},
		},
		{
			name:        "GoodBoth",
			credentials: &checker.Credentials{Client: "client1"},
			data: []*rules.SignData{
				{
					Data: []byte{
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
					},
					Domain: []byte{
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
					},
				},
			},
			accountNames: []string{"Test wallet/Test account 1"},
			pubKeys:      [][]byte{pubKeys["Test account 1"]},
			res:          []core.Result{core.ResultSucceeded},
		},
		{
			name:        "Duplicate",
			credentials: &checker.Credentials{Client: "client1"},
			data: []*rules.SignData{
				{
					Data: []byte{
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
					},
					Domain: []byte{
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
					},
				},
				{
					Data: []byte{
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
					},
					Domain: []byte{
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
					},
				},
			},
			accountNames: []string{"Test wallet/Test account 1", "Test wallet/Test account 1"},
			res:          []core.Result{core.ResultFailed, core.ResultFailed},
			logEntry:     "Multiple requests for same key",
		},
		{
			name: "DeniedClient",
			data: []*rules.SignData{
				{
					Data: []byte{
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
						0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01,
					},
					Domain: []byte{
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
						0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02, 0x02,
					},
				},
			},
			credentials:  &checker.Credentials{Client: "Deny this client"},
			accountNames: []string{"Test wallet/Test account 1"},
			res:          []core.Result{core.ResultDenied},
			logEntry:     "Negative permission matched",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			zerolog.SetGlobalLevel(zerolog.TraceLevel)
			capture := logger.NewLogCapture()

			checkerSvc, err := mockchecker.New(zerolog.TraceLevel)
			require.NoError(t, err)

			rulerSvc, err := golang.New(ctx,
				golang.WithLocker(lockerSvc),
				golang.WithRules(mockrules.New()))
			require.NoError(t, err)

			signerSvc, err := standardsigner.New(ctx,
				standardsigner.WithChecker(checkerSvc),
				standardsigner.WithFetcher(fetcherSvc),
				standardsigner.WithRuler(rulerSvc),
				standardsigner.WithUnlocker(unlockerSvc))
			require.NoError(t, err)

			res, _ := signerSvc.Multisign(context.Background(), test.credentials, test.accountNames, test.pubKeys, test.data)
			require.Equal(t, test.res, res)
			if test.logEntry != "" {
				capture.AssertHasEntry(t, test.logEntry)
			}
		})
	}
}
