// Copyright 2022 Gravitational, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package native

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/google/go-attestation/attest"
	"github.com/gravitational/trace"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"

	devicepb "github.com/gravitational/teleport/api/gen/proto/go/teleport/devicetrust/v1"
	"github.com/gravitational/teleport/lib/linux"
)

// deviceStateFolderName starts without a "." on Linux systems.
const deviceStateFolderName = "teleport-device"

var linuxDevice = &tpmDevice{
	isElevatedProcess: func() (bool, error) {
		// Always run TPM operations in-process.
		// The Linux impl will selectively escalate, via sudo, if necessary.
		return true, nil
	},
	activateCredentialInElevatedChild: func(encryptedCredential attest.EncryptedCredential, credActivationPath string, debug bool) ([]byte, error) {
		return nil, errors.New("elevated credential activation not implemented for linux")
	},
}

func enrollDeviceInit() (*devicepb.EnrollDeviceInit, error) {
	init, err := linuxDevice.enrollDeviceInit()
	return init, rewriteTPMPermissionError(err)
}

func signChallenge(chal []byte) (sig []byte, err error) {
	return nil, errors.New("signChallenge not implemented for TPM devices")
}

func getDeviceCredential() (*devicepb.DeviceCredential, error) {
	cred, err := linuxDevice.getDeviceCredential()
	return cred, rewriteTPMPermissionError(err)
}

func solveTPMEnrollChallenge(
	chal *devicepb.TPMEnrollChallenge,
	debug bool,
) (*devicepb.TPMEnrollChallengeResponse, error) {
	// No need to call rewriteTPMPermissionError here, enrollDeviceInit must pass
	// first.
	return linuxDevice.solveTPMEnrollChallenge(chal, debug)
}

func solveTPMAuthnDeviceChallenge(
	chal *devicepb.TPMAuthenticateDeviceChallenge,
) (*devicepb.TPMAuthenticateDeviceChallengeResponse, error) {
	resp, err := linuxDevice.solveTPMAuthnDeviceChallenge(chal)
	return resp, rewriteTPMPermissionError(err)
}

func handleTPMActivateCredential(encryptedCredential, encryptedCredentialSecret string) error {
	return errors.New("elevated credential activation not implemented for linux")
}

func rewriteTPMPermissionError(err error) error {
	// We are looking for an error that looks roughly like this:
	//
	// 	err = &fs.PathError{
	// 		Path: "/dev/tpmrm0",
	// 		Err: fs.ErrPermission,
	// 	}
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}

	pathErr := &fs.PathError{}
	if !errors.As(err, &pathErr) || pathErr.Path != "/dev/tpmrm0" {
		return err
	}
	log.
		WithError(err).
		Debug("TPM: Replacing TPM permission error with a more friendly one")

	return errors.New("" +
		"Failed to open the TPM device. " +
		"Consider assigning the user to the `tss` group or creating equivalent udev rules. " +
		"See https://goteleport.com/docs/access-controls/device-trust/device-management/#troubleshooting.")
}

// cddFuncs is used to mock various data collection functions for testing.
var cddFuncs = struct {
	parseOSRelease       func() (*linux.OSRelease, error)
	dmiInfoFromSysfs     func() (*linux.DMIInfo, error)
	readDMIInfoCached    func() (*linux.DMIInfo, error)
	readDMIInfoEscalated func() (*linux.DMIInfo, error)
	saveDMIInfoToCache   func(*linux.DMIInfo) error
}{
	parseOSRelease:       linux.ParseOSRelease,
	dmiInfoFromSysfs:     linux.DMIInfoFromSysfs,
	readDMIInfoCached:    readDMIInfoCached,
	readDMIInfoEscalated: readDMIInfoEscalated,
	saveDMIInfoToCache:   saveDMIInfoToCache,
}

func collectDeviceData(mode CollectDataMode) (*devicepb.DeviceCollectedData, error) {
	// Read collected data concurrently.
	//
	// We only have parseOSRelease and readDMIInfoAccordingToMode to consider, the
	// latter which is already concurrent internally, so a simple channel will do
	// here.
	//
	// Note that user.Current() is likely cached at this point.
	osReleaseC := make(chan *linux.OSRelease, 1 /* goroutine always completes */)
	go func() {
		osRelease, err := cddFuncs.parseOSRelease()
		if err != nil {
			log.WithError(err).Debug("TPM: Failed to parse /etc/os-release file")
			// err swallowed on purpose.

			osRelease = &linux.OSRelease{}
		}
		osReleaseC <- osRelease
	}()

	dmiInfo, err := readDMIInfoAccordingToMode(mode)
	if err != nil {
		// readDMIInfoAccordingToMode only errors if it fails completely.
		return nil, trace.Wrap(err)
	}

	// dmiInfo is expected to never be nil, but code defensively just in case.
	var modelIdentifier, reportedAssetTag, systemSerialNumber, baseBoardSerialNumber string
	if dmiInfo != nil {
		modelIdentifier = dmiInfo.ProductName
		reportedAssetTag = dmiInfo.ChassisAssetTag
		systemSerialNumber = dmiInfo.ProductSerial
		baseBoardSerialNumber = dmiInfo.BoardSerial
	}

	u, err := user.Current()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	osRelease := <-osReleaseC

	return &devicepb.DeviceCollectedData{
		CollectTime:           timestamppb.Now(),
		OsType:                devicepb.OSType_OS_TYPE_LINUX,
		SerialNumber:          firstValidAssetTag(reportedAssetTag, systemSerialNumber, baseBoardSerialNumber),
		ModelIdentifier:       modelIdentifier,
		OsId:                  osRelease.ID,
		OsVersion:             osRelease.VersionID,
		OsBuild:               osRelease.Version,
		OsUsername:            u.Name,
		ReportedAssetTag:      reportedAssetTag,
		SystemSerialNumber:    systemSerialNumber,
		BaseBoardSerialNumber: baseBoardSerialNumber,
	}, nil
}

func readDMIInfoAccordingToMode(mode CollectDataMode) (*linux.DMIInfo, error) {
	dmiInfo, err := cddFuncs.dmiInfoFromSysfs()
	if err == nil {
		return dmiInfo, nil
	}

	log.WithError(err).Warn("TPM: Failed to read device model and/or serial numbers")
	if !errors.Is(err, fs.ErrPermission) {
		return dmiInfo, nil // original info
	}

	switch mode {
	case CollectedDataNeverEscalate, CollectedDataMaybeEscalate:
		log.Debug("TPM: Reading cached DMI info")

		dmiCached, err := cddFuncs.readDMIInfoCached()
		if err == nil {
			return dmiCached, nil // successful cache hit
		}

		log.WithError(err).Debug("TPM: Failed to read cached DMI info")
		if mode == CollectedDataNeverEscalate {
			return dmiInfo, nil // original info
		}

		fallthrough

	case CollectedDataAlwaysEscalate:
		log.Debug("TPM: Running escalated `tsh device dmi-info`")

		dmiInfo, err = cddFuncs.readDMIInfoEscalated()
		if err != nil {
			return nil, trace.Wrap(err) // actual failure, abort
		}

		if err := cddFuncs.saveDMIInfoToCache(dmiInfo); err != nil {
			log.WithError(err).Warn("TPM: Failed to write DMI cache")
			// err swallowed on purpose.
		}
	}

	return dmiInfo, nil // escalated info or unknown mode
}

func readDMIInfoCached() (*linux.DMIInfo, error) {
	stateDir, err := setupDeviceStateDir(userDirFunc)
	if err != nil {
		return nil, trace.Wrap(err, "setting up state dir")
	}

	f, err := os.Open(stateDir.dmiJSONPath)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer f.Close()

	var dmiInfo linux.DMIInfo
	err = json.NewDecoder(f).Decode(&dmiInfo)
	return &dmiInfo, trace.Wrap(err)
}

func readDMIInfoEscalated() (*linux.DMIInfo, error) {
	tshPath, err := os.Executable()
	if err != nil {
		return nil, trace.Wrap(err, "reading current executable")
	}

	// Run `sudo -v` first to re-authenticate, then run the actual tsh command
	// using `sudo --non-interactive`, so we don't risk getting sudo output
	// mixed with our desired output.
	sudoCmd := exec.Command("/usr/bin/sudo", "-v")
	sudoCmd.Stdout = os.Stdout
	sudoCmd.Stderr = os.Stderr
	sudoCmd.Stdin = os.Stdin
	fmt.Println("Determining machine model and serial number, if prompted please type the sudo password")
	if err := sudoCmd.Run(); err != nil {
		return nil, trace.Wrap(err, "running `sudo -v`")
	}

	// Use a context for the cached sudo invocation. Unlike the previous command,
	// this shouldn't require any user input, thus it's expected to run fast.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dmiOut := &bytes.Buffer{}
	dmiCmd := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", tshPath, "device", "dmi-read")
	dmiCmd.Stdout = dmiOut
	if err := dmiCmd.Run(); err != nil {
		return nil, trace.Wrap(err, "running `sudo tsh device dmi-read`")
	}

	// Strip any leading output before the first `{`, just in case.
	val := dmiOut.String()
	if n := strings.Index(val, "{"); n > 0 {
		val = val[n-1:]
	}

	var dmiInfo linux.DMIInfo
	if err := json.Unmarshal([]byte(val), &dmiInfo); err != nil {
		return nil, trace.Wrap(err, "parsing dmi-read output")
	}

	return &dmiInfo, nil
}

func saveDMIInfoToCache(dmiInfo *linux.DMIInfo) error {
	stateDir, err := setupDeviceStateDir(userDirFunc)
	if err != nil {
		return trace.Wrap(err, "setting up state dir")
	}

	f, err := os.OpenFile(stateDir.dmiJSONPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return trace.Wrap(err, "opening dmi.json for write")
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(dmiInfo); err != nil {
		return trace.Wrap(err, "writing dmi.json")
	}
	if err := f.Close(); err != nil {
		return trace.Wrap(err, "closing dmi.json after write")
	}
	log.Debug("TPM: Saved DMI information to local cache")

	return nil
}
