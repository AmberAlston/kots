package cli

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/replicatedhq/kots/pkg/upload"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func UploadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "upload [path]",
		Short:         "",
		Long:          ``,
		SilenceUsage:  true,
		SilenceErrors: false,
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			v := viper.GetViper()

			if len(args) == 0 {
				cmd.Help()
				os.Exit(1)
			}

			uploadOptions := upload.UploadOptions{
				Namespace:       v.GetString("namespace"),
				Kubeconfig:      v.GetString("kubeconfig"),
				ExistingAppSlug: v.GetString("slug"),
				NewAppName:      v.GetString("name"),
			}

			if err := upload.Upload(ExpandDir(args[0]), uploadOptions); err != nil {
				return errors.Cause(err)
			}

			return nil
		},
	}

	cmd.Flags().String("kubeconfig", filepath.Join(homeDir(), ".kube", "config"), "the kubeconfig to use")
	cmd.Flags().String("namespace", "default", "the namespace to upload to")
	cmd.Flags().String("slug", "", "the application slug to use. if not present, a new one will be created")
	cmd.Flags().String("name", "", "the name of the kotsadm application to create")

	return cmd
}
