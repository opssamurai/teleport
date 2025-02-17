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

import React from 'react';

import { Text, Toggle, Link, Flex } from 'design';

import { ToolTipInfo } from 'shared/components/ToolTip';

const GUIDE_URL =
  'https://goteleport.com/docs/reference/predicate-language/#resource-filtering';

export function AdvancedSearchToggle(props: {
  isToggled: boolean;
  onToggle(): void;
  px?: number | string;
  gap?: number;
  className?: string;
}) {
  const gap = props.gap || 2;
  return (
    <Flex
      gap={gap}
      alignItems="center"
      px={props.px}
      className={props.className}
    >
      <Toggle isToggled={props.isToggled} onToggle={props.onToggle} />
      <Text typography="body2">Advanced</Text>
      <ToolTipInfo trigger="click">
        <PredicateDocumentation />
      </ToolTipInfo>
    </Flex>
  );
}

function PredicateDocumentation() {
  return (
    <>
      <Text typography="paragraph2" id="predicate-documentation">
        Advanced search allows you to perform more sophisticated searches using
        the predicate language. The language supports the basic operators:{' '}
        <Text as="span" bold>
          <code>==</code>{' '}
        </Text>
        ,{' '}
        <Text as="span" bold>
          <code>!=</code>
        </Text>
        ,{' '}
        <Text as="span" bold>
          <code>&&</code>
        </Text>
        , and{' '}
        <Text as="span" bold>
          <code>||</code>
        </Text>
      </Text>
      <Text typography="h4" mt={2} mb={1}>
        Usage Examples
      </Text>
      <Text typography="paragraph2">
        Label Matching:{' '}
        <Text ml={1} as="span" bold>
          <code>labels["key"] == "value" && labels["key2"] != "value2"</code>{' '}
        </Text>
        <br />
        Fuzzy Searching:{' '}
        <Text ml={1} as="span" bold>
          <code>search("foo", "bar", "some phrase")</code>
        </Text>
        <br />
        Combination:{' '}
        <Text ml={1} as="span" bold>
          <code>labels["key1"] == "value1" && search("foo")</code>
        </Text>
      </Text>
      <Text typography="paragraph2" mt={2}>
        Check out our{' '}
        <Link href={GUIDE_URL} target="_blank">
          predicate language guide
        </Link>{' '}
        for a more in-depth explanation of the language.
      </Text>
    </>
  );
}
