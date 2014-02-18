package main

import (
	"fmt"
	"github.com/gonuts/commander"
	"github.com/gonuts/flag"
	"github.com/smira/aptly/debian"
	"github.com/smira/aptly/utils"
	"sort"
	"strings"
)

func getVerifier(cmd *commander.Command) (utils.Verifier, error) {
	if utils.Config.GpgDisableVerify || cmd.Flag.Lookup("ignore-signatures").Value.Get().(bool) {
		return nil, nil
	}

	verifier := &utils.GpgVerifier{}
	for _, keyRing := range keyRings.keyRings {
		verifier.AddKeyring(keyRing)
	}

	err := verifier.InitKeyring()
	if err != nil {
		return nil, err
	}

	return verifier, nil
}

type keyRingsFlag struct {
	keyRings []string
}

func (k *keyRingsFlag) Set(value string) error {
	k.keyRings = append(k.keyRings, value)
	return nil
}

func (k *keyRingsFlag) Get() interface{} {
	return k.keyRings
}

func (k *keyRingsFlag) String() string {
	return strings.Join(k.keyRings, ",")
}

var keyRings = keyRingsFlag{}

func aptlyMirrorList(cmd *commander.Command, args []string) error {
	var err error
	if len(args) != 0 {
		cmd.Usage()
		return err
	}

	repoCollection := debian.NewRemoteRepoCollection(context.database)

	if repoCollection.Len() > 0 {
		fmt.Printf("List of mirrors:\n")
		repos := make([]string, repoCollection.Len())
		i := 0
		repoCollection.ForEach(func(repo *debian.RemoteRepo) error {
			repos[i] = repo.String()
			i++
			return nil
		})

		sort.Strings(repos)
		for _, repo := range repos {
			fmt.Printf(" * %s\n", repo)
		}

		fmt.Printf("\nTo get more information about mirror, run `aptly mirror show <name>`.\n")
	} else {
		fmt.Printf("No mirrors found, create one with `aptly mirror create ...`.\n")
	}
	return err
}

func aptlyMirrorCreate(cmd *commander.Command, args []string) error {
	var err error
	if len(args) < 3 {
		cmd.Usage()
		return err
	}

	downloadSources := utils.Config.DownloadSourcePackages || cmd.Flag.Lookup("with-sources").Value.Get().(bool)

	repo, err := debian.NewRemoteRepo(args[0], args[1], args[2], args[3:], context.architecturesList, downloadSources)
	if err != nil {
		return fmt.Errorf("unable to create mirror: %s", err)
	}

	verifier, err := getVerifier(cmd)
	if err != nil {
		return fmt.Errorf("unable to initialize GPG verifier: %s", err)
	}

	err = repo.Fetch(context.downloader, verifier)
	if err != nil {
		return fmt.Errorf("unable to fetch mirror: %s", err)
	}

	repoCollection := debian.NewRemoteRepoCollection(context.database)

	err = repoCollection.Add(repo)
	if err != nil {
		return fmt.Errorf("unable to add mirror: %s", err)
	}

	fmt.Printf("\nMirror %s successfully added.\nYou can run 'aptly mirror update %s' to download repository contents.\n", repo, repo.Name)
	return err
}

func aptlyMirrorShow(cmd *commander.Command, args []string) error {
	var err error
	if len(args) != 1 {
		cmd.Usage()
		return err
	}

	name := args[0]

	repoCollection := debian.NewRemoteRepoCollection(context.database)
	repo, err := repoCollection.ByName(name)
	if err != nil {
		return fmt.Errorf("unable to show: %s", err)
	}

	err = repoCollection.LoadComplete(repo)
	if err != nil {
		return fmt.Errorf("unable to show: %s", err)
	}

	fmt.Printf("Name: %s\n", repo.Name)
	fmt.Printf("Archive Root URL: %s\n", repo.ArchiveRoot)
	fmt.Printf("Distribution: %s\n", repo.Distribution)
	fmt.Printf("Components: %s\n", strings.Join(repo.Components, ", "))
	fmt.Printf("Architectures: %s\n", strings.Join(repo.Architectures, ", "))
	downloadSources := "no"
	if repo.DownloadSources {
		downloadSources = "yes"
	}
	fmt.Printf("Download Sources: %s\n", downloadSources)
	if repo.LastDownloadDate.IsZero() {
		fmt.Printf("Last update: never\n")
	} else {
		fmt.Printf("Last update: %s\n", repo.LastDownloadDate.Format("2006-01-02 15:04:05 MST"))
		fmt.Printf("Number of packages: %d\n", repo.NumPackages())
	}

	fmt.Printf("\nInformation from release file:\n")
	for _, k := range utils.StrMapSortedKeys(repo.Meta) {
		fmt.Printf("%s: %s\n", k, repo.Meta[k])
	}

	withPackages := cmd.Flag.Lookup("with-packages").Value.Get().(bool)
	if withPackages {
		if repo.LastDownloadDate.IsZero() {
			fmt.Printf("Unable to show package list, mirror hasn't been downloaded yet.\n")
		} else {
			ListPackagesRefList(repo.RefList())
		}
	}

	return err
}

func aptlyMirrorUpdate(cmd *commander.Command, args []string) error {
	var err error
	if len(args) != 1 {
		cmd.Usage()
		return err
	}

	name := args[0]

	repoCollection := debian.NewRemoteRepoCollection(context.database)
	repo, err := repoCollection.ByName(name)
	if err != nil {
		return fmt.Errorf("unable to update: %s", err)
	}

	err = repoCollection.LoadComplete(repo)
	if err != nil {
		return fmt.Errorf("unable to update: %s", err)
	}

	ignoreMismatch := cmd.Flag.Lookup("ignore-checksums").Value.Get().(bool)

	verifier, err := getVerifier(cmd)
	if err != nil {
		return fmt.Errorf("unable to initialize GPG verifier: %s", err)
	}

	err = repo.Fetch(context.downloader, verifier)
	if err != nil {
		return fmt.Errorf("unable to update: %s", err)
	}

	packageCollection := debian.NewPackageCollection(context.database)

	err = repo.Download(context.downloader, packageCollection, context.packageRepository, ignoreMismatch)
	if err != nil {
		return fmt.Errorf("unable to update: %s", err)
	}

	err = repoCollection.Update(repo)
	if err != nil {
		return fmt.Errorf("unable to update: %s", err)
	}

	fmt.Printf("\nMirror `%s` has been successfully updated.\n", repo.Name)
	return err
}

func aptlyMirrorDrop(cmd *commander.Command, args []string) error {
	var err error
	if len(args) != 1 {
		cmd.Usage()
		return err
	}

	name := args[0]

	repoCollection := debian.NewRemoteRepoCollection(context.database)
	repo, err := repoCollection.ByName(name)
	if err != nil {
		return fmt.Errorf("unable to drop: %s", err)
	}

	force := cmd.Flag.Lookup("force").Value.Get().(bool)
	if !force {
		snapshotCollection := debian.NewSnapshotCollection(context.database)
		snapshots := snapshotCollection.ByRemoteRepoSource(repo)

		if len(snapshots) > 0 {
			fmt.Printf("Mirror `%s` was used to create following snapshots:\n", repo.Name)
			for _, snapshot := range snapshots {
				fmt.Printf(" * %s\n", snapshot)
			}

			return fmt.Errorf("won't delete mirror with snapshots, use -force to override")
		}
	}

	err = repoCollection.Drop(repo)
	if err != nil {
		return fmt.Errorf("unable to drop: %s", err)
	}

	fmt.Printf("Mirror `%s` has been removed.\n", repo.Name)

	return err
}

func makeCmdMirrorCreate() *commander.Command {
	cmd := &commander.Command{
		Run:       aptlyMirrorCreate,
		UsageLine: "create <name> <archive url> <distribution> [<component1> ...]",
		Short:     "create new mirror of Debian repository",
		Long: `
Create records information about new mirror and fetches Release file (it doesn't download packages).

ex:
  $ aptly mirror create wheezy-main http://mirror.yandex.ru/debian/ wheezy main
`,
		Flag: *flag.NewFlagSet("aptly-mirror-create", flag.ExitOnError),
	}

	cmd.Flag.Bool("ignore-signatures", false, "disable verification of Release file signatures")
	cmd.Flag.Bool("with-sources", false, "download source packages")
	cmd.Flag.Var(&keyRings, "keyring", "gpg keyring to use when verifying Release file (could be specified multiple times)")

	return cmd
}

func makeCmdMirrorList() *commander.Command {
	cmd := &commander.Command{
		Run:       aptlyMirrorList,
		UsageLine: "list",
		Short:     "list mirrors of remote repositories",
		Long: `
List shows full list of remote repositories.

ex:
  $ aptly mirror list
`,
		Flag: *flag.NewFlagSet("aptly-mirror-list", flag.ExitOnError),
	}
	cmd.Flag.Bool("v", false, "enable verbose output")

	return cmd
}

func makeCmdMirrorShow() *commander.Command {
	cmd := &commander.Command{
		Run:       aptlyMirrorShow,
		UsageLine: "show <name>",
		Short:     "show details about remote repository mirror",
		Long: `
Show shows full information about mirror.

ex:
  $ aptly mirror show wheezy-main
`,
		Flag: *flag.NewFlagSet("aptly-mirror-show", flag.ExitOnError),
	}

	cmd.Flag.Bool("with-packages", false, "show list of packages")

	return cmd
}

func makeCmdMirrorUpdate() *commander.Command {
	cmd := &commander.Command{
		Run:       aptlyMirrorUpdate,
		UsageLine: "update <name>",
		Short:     "update packages from remote mirror",
		Long: `
Update downloads list of packages and package files.

ex:
  $ aptly mirror update wheezy-main
`,
		Flag: *flag.NewFlagSet("aptly-mirror-update", flag.ExitOnError),
	}

	cmd.Flag.Bool("ignore-checksums", false, "ignore checksum mismatches while downloading package files and metadata")
	cmd.Flag.Bool("ignore-signatures", false, "disable verification of Release file signatures")
	cmd.Flag.Var(&keyRings, "keyring", "gpg keyring to use when verifying Release file (could be specified multiple times)")

	return cmd
}

func makeCmdMirrorDrop() *commander.Command {
	cmd := &commander.Command{
		Run:       aptlyMirrorDrop,
		UsageLine: "drop <name>",
		Short:     "delete remote repository mirror",
		Long: `
Drop deletes information about remote repository mirror. Package data is not deleted
(it could be still used by other mirrors or snapshots).

ex:
  $ aptly mirror drop wheezy-main
`,
		Flag: *flag.NewFlagSet("aptly-mirror-drop", flag.ExitOnError),
	}

	cmd.Flag.Bool("force", false, "force mirror deletion even if used by snapshots")

	return cmd
}

func makeCmdMirror() *commander.Command {
	return &commander.Command{
		UsageLine: "mirror",
		Short:     "manage mirrors of remote repositories",
		Subcommands: []*commander.Command{
			makeCmdMirrorCreate(),
			makeCmdMirrorList(),
			makeCmdMirrorShow(),
			makeCmdMirrorDrop(),
			makeCmdMirrorUpdate(),
		},
		Flag: *flag.NewFlagSet("aptly-mirror", flag.ExitOnError),
	}
}
