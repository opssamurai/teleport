/**
 * Copyright 2023 Gravitational, Inc
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import React, { useEffect, useCallback, useState, useRef } from 'react';
import { Link } from 'react-router-dom';

import { ButtonSecondary } from 'design/Button';
import { Platform, getPlatform } from 'design/platform';
import { Text, Flex } from 'design';
import * as Icons from 'design/Icon';
import { MenuButton, MenuItem } from 'shared/components/MenuAction';
import { Path, makeDeepLinkWithSafeInput } from 'shared/deepLinks';
import * as constants from 'shared/constants';

import cfg from 'teleport/config';
import useTeleport from 'teleport/useTeleport';
import { Node } from 'teleport/services/nodes';

import {
  ActionButtons,
  Header,
  StyledBox,
  TextIcon,
} from 'teleport/Discover/Shared';
import { usePoll } from 'teleport/Discover/Shared/usePoll';

import {
  HintBox,
  SuccessBox,
  WaitingInfo,
} from 'teleport/Discover/Shared/HintBox';

import type { AgentStepProps } from '../../types';

export function SetupConnect(
  props: AgentStepProps & {
    pingInterval?: number;
    showHintTimeout?: number;
  }
) {
  const pingInterval = props.pingInterval || 1000 * 3; // 3 seconds
  const showHintTimeout = props.showHintTimeout || 1000 * 60 * 5; // 5 minutes

  const ctx = useTeleport();
  const clusterId = ctx.storeUser.getClusterId();
  const { cluster, username } = ctx.storeUser.state;
  const platform = getPlatform();
  const downloadLinks = getConnectDownloadLinks(platform, cluster.proxyVersion);
  const connectMyComputerDeepLink = makeDeepLinkWithSafeInput({
    proxyHost: cluster.publicURL,
    username,
    path: Path.ConnectMyComputer,
  });
  const [showHint, setShowHint] = useState(false);

  const { node, isPolling } = usePollForConnectMyComputerNode({
    username,
    clusterId,
    // If reloadUser is set to true, the polling callback takes longer to finish so let's increase
    // the polling interval as well.
    pingInterval: showHint ? pingInterval * 2 : pingInterval,
    // Completing the Connect My Computer setup in Connect causes the user to gain a new role. That
    // role grants access to nodes labeled with `teleport.dev/connect-my-computer/owner:
    // <current-username>`.
    //
    // In certain cases, that role might be the only role which grants the user the visibility of
    // the Connect My Computer node. For example, if the user doesn't have a role like the built-in
    // access which gives blanket access to all nodes, the user won't be able to see the node until
    // they have the Connect My Computer role in their cert.
    //
    // As such, if we don't reload the cert during polling, it might never see the node. So let's
    // flip it to true after a timeout.
    reloadUser: showHint,
  });

  // TODO(ravicious): Take these from the context rather than from props.
  const { agentMeta, updateAgentMeta, nextStep } = props;
  const handleNextStep = () => {
    if (!node) {
      return;
    }

    updateAgentMeta({
      ...agentMeta,
      // Node is an oddity in that the hostname is the more
      // user identifiable resource name and what user expects
      // as the resource name.
      resourceName: node.hostname,
      node,
    });
    nextStep();
  };

  useEffect(() => {
    if (isPolling) {
      const id = window.setTimeout(() => setShowHint(true), showHintTimeout);

      return () => window.clearTimeout(id);
    }
  }, [isPolling, showHintTimeout]);

  let pollingStatus: JSX.Element;
  if (showHint && !node) {
    pollingStatus = (
      // Override max-width to match StyledBox's max-width.
      <HintBox header="We're still looking for your computer" maxWidth="800px">
        <Flex flexDirection="column" gap={3}>
          <Text>
            There are a couple of possible reasons for why we haven't been able
            to detect your computer.
          </Text>

          <ul
            css={`
              margin: 0;
              padding-left: ${p => p.theme.space[3]}px;
            `}
          >
            <li>
              <Text>
                You did not start Connect My Computer in Teleport Connect yet.
              </Text>
            </li>
            <li>
              <Text>
                The Teleport agent started by Teleport Connect could not join
                this Teleport cluster. Check if the Connect My Computer tab in
                Teleport Connect shows any error messages.
              </Text>
            </li>
            <li>
              <Text>
                The computer you are trying to add has already joined the
                Teleport cluster before you entered this page. If that's the
                case, you can go back to{' '}
                <Link to={cfg.getUnifiedResourcesRoute(clusterId)}>
                  the resources
                </Link>{' '}
                and connect to it.
              </Text>
            </li>
          </ul>

          <Text>
            We'll continue to look for the computer whilst you diagnose the
            issue.
          </Text>
        </Flex>
      </HintBox>
    );
  } else if (node) {
    pollingStatus = (
      <SuccessBox>
        <Text>
          Your computer, <strong>{node.hostname}</strong>, has been detected!
        </Text>
      </SuccessBox>
    );
  } else {
    pollingStatus = (
      <WaitingInfo>
        <TextIcon
          css={`
            white-space: pre;
          `}
        >
          <Icons.Restore size="medium" mr={2} />
        </TextIcon>
        After your computer is connected to the cluster, we’ll automatically
        detect it.
      </WaitingInfo>
    );
  }

  return (
    <Flex flexDirection="column" alignItems="flex-start" mb={2} gap={4}>
      <Header>Set Up Teleport Connect</Header>

      <StyledBox>
        <Text bold>Step 1: Download and Install Teleport Connect</Text>

        <Text typography="subtitle1" mb={2}>
          Teleport Connect is a native desktop application for browsing and
          accessing your resources. It can also connect your computer as an SSH
          resource and scope access to a unique role so it is not automatically
          shared with all users in the&nbsp;cluster.
          <br />
          <br />
          Once you’ve downloaded Teleport Connect, run the installer to add it
          to your computer’s applications.
        </Text>

        <Flex flexWrap="wrap" alignItems="baseline" gap={2}>
          <DownloadConnect downloadLinks={downloadLinks} />
          <Text typography="subtitle1">
            Already have Teleport Connect? Skip to the next step.
          </Text>
        </Flex>
      </StyledBox>

      <StyledBox>
        <Text bold>Step 2: Sign In and Connect My Computer</Text>

        <Text typography="subtitle1" mb={2}>
          The button below will open Teleport Connect and once you are logged
          in, it will prompt you to connect your computer. From there, follow
          the instructions in Teleport Connect, and this page will update when
          your computer is detected in the cluster.
        </Text>

        <ButtonSecondary as="a" href={connectMyComputerDeepLink}>
          Sign In & Connect My Computer
        </ButtonSecondary>
      </StyledBox>

      {pollingStatus}

      <ActionButtons
        onProceed={handleNextStep}
        disableProceed={!node}
        onPrev={props.prevStep}
      />
    </Flex>
  );
}

/**
 * usePollForConnectMyComputerNode polls for a Connect My Computer node that joined the cluster
 * after starting opening the SetupConnect step.
 *
 * The first polling request fills out a set of node IDs (initialNodeIdsRef). Subsequent requests
 * check the returned nodes against this set. The hook stops polling as soon as a node that is not
 * in the set was found.
 *
 * There can be multiple nodes matching the search criteria and we want the one that was added only
 * after the user has started the guided flow, hence why we need to keep track of the IDs in a set.
 *
 * Unlike the DownloadScript step responsible for adding a server, we don't have a unique ID that
 * identifies the node that the user added after following the steps from the guided flow. In
 * theory, we could make the deep link button pass such ID to Connect, but the user would still be
 * able to just launch the app directly and not use the link.
 *
 * Because of that, we must depend on comparing the list of nodes against the initial set of IDs.
 */
export const usePollForConnectMyComputerNode = (args: {
  username: string;
  clusterId: string;
  reloadUser: boolean;
  pingInterval: number;
}): {
  node: Node | undefined;
  isPolling: boolean;
} => {
  const ctx = useTeleport();
  const [isPolling, setIsPolling] = useState(true);
  const initialNodeIdsRef = useRef<Set<string>>(null);

  const node = usePoll(
    useCallback(
      async signal => {
        if (args.reloadUser) {
          await ctx.userService.reloadUser(signal);
        }

        const request = {
          query: `labels["${constants.ConnectMyComputerNodeOwnerLabel}"] == "${args.username}"`,
          // An arbitrary limit where we bank on the fact that no one is going to have 50 Connect My
          // Computer nodes assigned to them running at the same time.
          limit: 50,
        };

        const response = await ctx.nodeService.fetchNodes(
          args.clusterId,
          request,
          signal
        );

        // Fill out the set with node IDs if it's empty.
        if (initialNodeIdsRef.current === null) {
          initialNodeIdsRef.current = new Set(
            response.agents.map(agent => agent.id)
          );
          return null;
        }

        // On subsequent requests, compare the nodes from the response against the set.
        const node = response.agents.find(
          agent => !initialNodeIdsRef.current.has(agent.id)
        );

        if (node) {
          setIsPolling(false);
          return node;
        }
      },
      [
        ctx.nodeService,
        ctx.userService,
        args.clusterId,
        args.username,
        args.reloadUser,
      ]
    ),
    isPolling,
    args.pingInterval
  );

  return { node, isPolling };
};

type DownloadLink = { text: string; url: string };

const DownloadConnect = (props: { downloadLinks: Array<DownloadLink> }) => {
  if (props.downloadLinks.length === 1) {
    const downloadLink = props.downloadLinks[0];
    return (
      <ButtonSecondary as="a" href={downloadLink.url}>
        Download Teleport Connect
      </ButtonSecondary>
    );
  }

  return (
    <MenuButton buttonText="Download Teleport Connect">
      {props.downloadLinks.map(link => (
        <MenuItem key={link.url} as="a" href={link.url}>
          {link.text}
        </MenuItem>
      ))}
    </MenuButton>
  );
};

function getConnectDownloadLinks(
  platform: Platform,
  proxyVersion: string
): Array<DownloadLink> {
  switch (platform) {
    case Platform.Windows:
      return [
        {
          text: 'Teleport Connect',
          url: `https://cdn.teleport.dev/Teleport Connect Setup-${proxyVersion}.exe`,
        },
      ];
    case Platform.macOS:
      return [
        {
          text: 'Teleport Connect',
          url: `https://cdn.teleport.dev/Teleport Connect-${proxyVersion}.dmg`,
        },
      ];
    case Platform.Linux:
      return [
        {
          text: 'DEB',
          url: `https://cdn.teleport.dev/teleport-connect_${proxyVersion}_amd64.deb`,
        },
        {
          text: 'RPM',
          url: `https://cdn.teleport.dev/teleport-connect-${proxyVersion}.x86_64.rpm`,
        },

        {
          text: 'tar.gz',
          url: `https://cdn.teleport.dev/teleport-connect-${proxyVersion}-x64.tar.gz`,
        },
      ];
  }
}
