// +build !remoteclient

package integration

import (
	"net"
	"os"
	"os/exec"

	"github.com/containers/libpod/pkg/criu"
	. "github.com/containers/libpod/test/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Podman checkpoint", func() {
	var (
		tempdir    string
		err        error
		podmanTest *PodmanTestIntegration
	)

	BeforeEach(func() {
		SkipIfRootless()
		tempdir, err = CreateTempDirInTempDir()
		if err != nil {
			os.Exit(1)
		}
		podmanTest = PodmanTestCreate(tempdir)
		podmanTest.Setup()
		podmanTest.SeedImages()
		// Check if the runtime implements checkpointing. Currently only
		// runc's checkpoint/restore implementation is supported.
		cmd := exec.Command(podmanTest.OCIRuntime, "checkpoint", "-h")
		if err := cmd.Start(); err != nil {
			Skip("OCI runtime does not support checkpoint/restore")
		}
		if err := cmd.Wait(); err != nil {
			Skip("OCI runtime does not support checkpoint/restore")
		}

		if !criu.CheckForCriu() {
			Skip("CRIU is missing or too old.")
		}
		// Only Fedora 29 and newer has a new enough selinux-policy and
		// container-selinux package to support CRIU in correctly
		// restoring threaded processes
		hostInfo := podmanTest.Host
		if hostInfo.Distribution == "fedora" && hostInfo.Version < "29" {
			Skip("Checkpoint/Restore with SELinux only works on Fedora >= 29")
		}

	})

	AfterEach(func() {
		podmanTest.Cleanup()
		f := CurrentGinkgoTestDescription()
		processTestResult(f)

	})

	It("podman checkpoint bogus container", func() {
		session := podmanTest.Podman([]string{"container", "checkpoint", "foobar"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Not(Equal(0)))
	})

	It("podman restore bogus container", func() {
		session := podmanTest.Podman([]string{"container", "restore", "foobar"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Not(Equal(0)))
	})

	It("podman checkpoint a running container by id", func() {
		// CRIU does not work with seccomp correctly on RHEL7
		session := podmanTest.Podman([]string{"run", "-it", "--security-opt", "seccomp=unconfined", "-d", ALPINE, "top"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))
		cid := session.OutputToString()

		result := podmanTest.Podman([]string{"container", "checkpoint", cid})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Exited"))

		result = podmanTest.Podman([]string{"container", "restore", cid})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(1))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Up"))
	})

	It("podman checkpoint a running container by name", func() {
		session := podmanTest.Podman([]string{"run", "-it", "--security-opt", "seccomp=unconfined", "--name", "test_name", "-d", ALPINE, "top"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))

		result := podmanTest.Podman([]string{"container", "checkpoint", "test_name"})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Exited"))

		result = podmanTest.Podman([]string{"container", "restore", "test_name"})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(1))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Up"))
	})

	It("podman pause a checkpointed container by id", func() {
		session := podmanTest.Podman([]string{"run", "-it", "--security-opt", "seccomp=unconfined", "-d", ALPINE, "top"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))
		cid := session.OutputToString()

		result := podmanTest.Podman([]string{"container", "checkpoint", cid})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Exited"))

		result = podmanTest.Podman([]string{"pause", cid})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(125))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Exited"))

		result = podmanTest.Podman([]string{"container", "restore", cid})
		result.WaitWithDefaultTimeout()
		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(1))

		result = podmanTest.Podman([]string{"rm", cid})
		result.WaitWithDefaultTimeout()
		Expect(result.ExitCode()).To(Equal(125))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(1))

		result = podmanTest.Podman([]string{"rm", "-f", cid})
		result.WaitWithDefaultTimeout()
		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))

	})

	It("podman checkpoint latest running container", func() {
		session1 := podmanTest.Podman([]string{"run", "-it", "--security-opt", "seccomp=unconfined", "--name", "first", "-d", ALPINE, "top"})
		session1.WaitWithDefaultTimeout()
		Expect(session1.ExitCode()).To(Equal(0))

		session2 := podmanTest.Podman([]string{"run", "-it", "--security-opt", "seccomp=unconfined", "--name", "second", "-d", ALPINE, "top"})
		session2.WaitWithDefaultTimeout()
		Expect(session2.ExitCode()).To(Equal(0))

		result := podmanTest.Podman([]string{"container", "checkpoint", "-l"})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(1))

		ps := podmanTest.Podman([]string{"ps", "-q", "--no-trunc"})
		ps.WaitWithDefaultTimeout()
		Expect(ps.ExitCode()).To(Equal(0))
		Expect(ps.LineInOutputContains(session1.OutputToString())).To(BeTrue())
		Expect(ps.LineInOutputContains(session2.OutputToString())).To(BeFalse())

		result = podmanTest.Podman([]string{"container", "restore", "-l"})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(2))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Up"))
		Expect(podmanTest.GetContainerStatus()).To(Not(ContainSubstring("Exited")))

		result = podmanTest.Podman([]string{"rm", "-fa"})
		result.WaitWithDefaultTimeout()
		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
	})

	It("podman checkpoint all running container", func() {
		session1 := podmanTest.Podman([]string{"run", "-it", "--security-opt", "seccomp=unconfined", "--name", "first", "-d", ALPINE, "top"})
		session1.WaitWithDefaultTimeout()
		Expect(session1.ExitCode()).To(Equal(0))

		session2 := podmanTest.Podman([]string{"run", "-it", "--security-opt", "seccomp=unconfined", "--name", "second", "-d", ALPINE, "top"})
		session2.WaitWithDefaultTimeout()
		Expect(session2.ExitCode()).To(Equal(0))

		result := podmanTest.Podman([]string{"container", "checkpoint", "-a"})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))

		ps := podmanTest.Podman([]string{"ps", "-q", "--no-trunc"})
		ps.WaitWithDefaultTimeout()
		Expect(ps.ExitCode()).To(Equal(0))
		Expect(ps.LineInOutputContains(session1.OutputToString())).To(BeFalse())
		Expect(ps.LineInOutputContains(session2.OutputToString())).To(BeFalse())

		result = podmanTest.Podman([]string{"container", "restore", "-a"})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(2))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Up"))
		Expect(podmanTest.GetContainerStatus()).To(Not(ContainSubstring("Exited")))

		result = podmanTest.Podman([]string{"rm", "-fa"})
		result.WaitWithDefaultTimeout()
		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
	})

	It("podman checkpoint container with established tcp connections", func() {
		session := podmanTest.Podman([]string{"run", "-it", "--security-opt", "seccomp=unconfined", "-d", redis})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))

		IP := podmanTest.Podman([]string{"inspect", "-l", "--format={{.NetworkSettings.IPAddress}}"})
		IP.WaitWithDefaultTimeout()
		Expect(IP.ExitCode()).To(Equal(0))

		// Open a network connection to the redis server
		conn, err := net.Dial("tcp", IP.OutputToString()+":6379")
		if err != nil {
			os.Exit(1)
		}
		// This should fail as the container has established TCP connections
		result := podmanTest.Podman([]string{"container", "checkpoint", "-l"})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(125))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(1))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Up"))

		// Now it should work thanks to "--tcp-established"
		result = podmanTest.Podman([]string{"container", "checkpoint", "-l", "--tcp-established"})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Exited"))

		// Restore should fail as the checkpoint image contains established TCP connections
		result = podmanTest.Podman([]string{"container", "restore", "-l"})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(125))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Exited"))

		// Now it should work thanks to "--tcp-established"
		result = podmanTest.Podman([]string{"container", "restore", "-l", "--tcp-established"})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(1))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Up"))

		result = podmanTest.Podman([]string{"rm", "-fa"})
		result.WaitWithDefaultTimeout()
		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))

		conn.Close()
	})

	It("podman checkpoint with --leave-running", func() {
		session := podmanTest.Podman([]string{"run", "-it", "--security-opt", "seccomp=unconfined", "-d", ALPINE, "top"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))
		cid := session.OutputToString()

		// Checkpoint container, but leave it running
		result := podmanTest.Podman([]string{"container", "checkpoint", "--leave-running", cid})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		// Make sure it is still running
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(1))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Up"))

		// Stop the container
		result = podmanTest.Podman([]string{"container", "stop", cid})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Exited"))

		// Restore the stopped container from the previous checkpoint
		result = podmanTest.Podman([]string{"container", "restore", cid})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(1))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Up"))

		result = podmanTest.Podman([]string{"rm", "-fa"})
		result.WaitWithDefaultTimeout()
		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
	})

	It("podman checkpoint and restore container with same IP", func() {
		session := podmanTest.Podman([]string{"run", "-it", "--security-opt", "seccomp=unconfined", "--name", "test_name", "-d", ALPINE, "top"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))

		IPBefore := podmanTest.Podman([]string{"inspect", "-l", "--format={{.NetworkSettings.IPAddress}}"})
		IPBefore.WaitWithDefaultTimeout()
		Expect(IPBefore.ExitCode()).To(Equal(0))

		result := podmanTest.Podman([]string{"container", "checkpoint", "test_name"})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Exited"))

		result = podmanTest.Podman([]string{"container", "restore", "test_name"})
		result.WaitWithDefaultTimeout()

		IPAfter := podmanTest.Podman([]string{"inspect", "-l", "--format={{.NetworkSettings.IPAddress}}"})
		IPAfter.WaitWithDefaultTimeout()
		Expect(IPAfter.ExitCode()).To(Equal(0))

		// Check that IP address did not change between checkpointing and restoring
		Expect(IPBefore.OutputToString()).To(Equal(IPAfter.OutputToString()))

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(1))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Up"))

		result = podmanTest.Podman([]string{"rm", "-fa"})
		result.WaitWithDefaultTimeout()
		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
	})

	// This test does the same steps which are necessary for migrating
	// a container from one host to another
	It("podman checkpoint container with export (migration)", func() {
		// CRIU does not work with seccomp correctly on RHEL7
		session := podmanTest.Podman([]string{"run", "-it", "--security-opt", "seccomp=unconfined", "-d", ALPINE, "top"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(1))

		result := podmanTest.Podman([]string{"container", "checkpoint", "-l", "-e", "/tmp/checkpoint.tar.gz"})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Exited"))

		// Remove all containers to simulate migration
		result = podmanTest.Podman([]string{"rm", "-fa"})
		result.WaitWithDefaultTimeout()
		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))

		result = podmanTest.Podman([]string{"container", "restore", "-i", "/tmp/checkpoint.tar.gz"})
		result.WaitWithDefaultTimeout()

		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(1))
		Expect(podmanTest.GetContainerStatus()).To(ContainSubstring("Up"))

		result = podmanTest.Podman([]string{"rm", "-fa"})
		result.WaitWithDefaultTimeout()
		Expect(result.ExitCode()).To(Equal(0))
		Expect(podmanTest.NumberOfContainersRunning()).To(Equal(0))
	})
})
