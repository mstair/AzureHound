// Copyright (C) 2022 Specter Ops, Inc.
//
// This file is part of AzureHound.
//
// AzureHound is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// AzureHound is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"time"

	"github.com/bloodhoundad/azurehound/client"
	"github.com/bloodhoundad/azurehound/constants"
	"github.com/bloodhoundad/azurehound/enums"
	"github.com/bloodhoundad/azurehound/models"
	"github.com/bloodhoundad/azurehound/pipeline"
	"github.com/spf13/cobra"
)

func init() {
	listRootCmd.AddCommand(listKeyVaultContributorsCmd)
}

var listKeyVaultContributorsCmd = &cobra.Command{
	Use:          "key-vault-contributors",
	Long:         "Lists Azure Key Vault Contributors",
	Run:          listKeyVaultContributorsCmdImpl,
	SilenceUsage: true,
}

func listKeyVaultContributorsCmdImpl(cmd *cobra.Command, args []string) {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, os.Kill)
	defer gracefulShutdown(stop)

	log.V(1).Info("testing connections")
	if err := testConnections(); err != nil {
		exit(err)
	} else if azClient, err := newAzureClient(); err != nil {
		exit(err)
	} else {
		log.Info("collecting azure key vault contributors...")
		start := time.Now()
		subscriptions := listSubscriptions(ctx, azClient)
		keyVaults := listKeyVaults(ctx, azClient, subscriptions)
		kvRoleAssignments := listKeyVaultRoleAssignments(ctx, azClient, keyVaults)
		stream := listKeyVaultContributors(ctx, azClient, kvRoleAssignments)
		outputStream(ctx, stream)
		duration := time.Since(start)
		log.Info("collection completed", "duration", duration.String())
	}
}

func listKeyVaultContributors(ctx context.Context, client client.AzureClient, vmRoleAssignments <-chan interface{}) <-chan interface{} {
	out := make(chan interface{})

	go func() {
		defer close(out)

		for result := range pipeline.OrDone(ctx.Done(), vmRoleAssignments) {
			if roleAssignments, ok := result.(AzureWrapper).Data.(models.KeyVaultRoleAssignments); !ok {
				log.Error(fmt.Errorf("failed type assertion"), "unable to continue enumerating key vault contributors", "result", result)
				return
			} else {
				var (
					keyVaultContributors = models.KeyVaultContributors{
						KeyVaultId: roleAssignments.KeyVaultId,
					}
					count = 0
				)
				for _, item := range roleAssignments.RoleAssignments {
					roleDefinitionId := path.Base(item.RoleAssignment.Properties.RoleDefinitionId)

					if roleDefinitionId == constants.ContributorRoleID {
						keyVaultContributor := models.KeyVaultContributor{
							Contributor: item.RoleAssignment,
							KeyVaultId:  item.KeyVaultId,
						}
						log.V(2).Info("found key vault contributor", "keyVaultContributor", keyVaultContributor)
						count++
						keyVaultContributors.Contributors = append(keyVaultContributors.Contributors, keyVaultContributor)
					}
				}
				out <- AzureWrapper{
					Kind: enums.KindAZVMContributor,
					Data: keyVaultContributors,
				}
				log.V(1).Info("finished listing key vault contributors", "keyVaultId", roleAssignments.KeyVaultId, "count", count)
			}
		}
		log.Info("finished listing all key vault contributors")
	}()

	return out
}
