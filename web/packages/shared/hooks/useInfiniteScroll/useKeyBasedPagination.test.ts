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

import { renderHook, act } from '@testing-library/react-hooks';

import { ApiError } from 'teleport/services/api/parseError';

import { Node } from 'teleport/services/nodes';

import {
  useKeyBasedPagination,
  KeyBasedPaginationOptions,
} from './useKeyBasedPagination';
import { newFetchFunc, resourceClusterIds, resourceNames } from './testUtils';

function hookProps(overrides: Partial<KeyBasedPaginationOptions<Node>> = {}) {
  return {
    fetchFunc: newFetchFunc({
      numResources: 7,
    }),
    filter: {},
    initialFetchSize: 2,
    fetchMoreSize: 3,
    ...overrides,
  };
}

test.each`
  n     | names
  ${3}  | ${['r0', 'r1', 'r2']}
  ${4}  | ${['r0', 'r1', 'r2', 'r3']}
  ${10} | ${['r0', 'r1', 'r2', 'r3']}
`('fetches one data batch, n=$n', async ({ n, names }) => {
  const { result } = renderHook(useKeyBasedPagination, {
    initialProps: hookProps({
      fetchFunc: newFetchFunc({
        numResources: 4,
      }),
      initialFetchSize: n,
    }),
  });

  expect(result.current.resources).toEqual([]);
  await act(result.current.fetch);
  expect(resourceNames(result)).toEqual(names);
});

test('fetches multiple data batches', async () => {
  const { result } = renderHook(useKeyBasedPagination, {
    initialProps: hookProps(),
  });
  expect(result.current.finished).toBe(false);

  await act(result.current.fetch);
  await act(result.current.fetch);
  expect(resourceNames(result)).toEqual(['r0', 'r1', 'r2', 'r3', 'r4']);
  expect(result.current.finished).toBe(false);
  await act(result.current.fetch);

  const allResources = ['r0', 'r1', 'r2', 'r3', 'r4', 'r5', 'r6'];
  expect(resourceNames(result)).toEqual(allResources);
  expect(result.current.finished).toBe(true);
  await act(result.current.fetch);
  expect(resourceNames(result)).toEqual(allResources);
  expect(result.current.finished).toBe(true);
});

test('maintains attempt state', async () => {
  const { result } = renderHook(useKeyBasedPagination, {
    initialProps: hookProps(),
  });

  expect(result.current.attempt.status).toBe('');
  let fetchPromise;
  act(() => {
    fetchPromise = result.current.fetch();
  });
  expect(result.current.attempt.status).toBe('processing');
  await act(async () => fetchPromise);
  expect(result.current.attempt.status).toBe('success');

  act(() => {
    fetchPromise = result.current.fetch();
  });
  expect(result.current.attempt.status).toBe('processing');
  await act(async () => fetchPromise);
  expect(result.current.attempt.status).toBe('success');
});

test('restarts after fetch function change', async () => {
  const updateSearch = (search: string) => {
    // clears resources before fetching new data
    act(() => result.current.clear());
    rerender({
      ...props,
      fetchFunc: newFetchFunc({
        search,
        numResources: 4,
      }),
    });
  };

  let props = hookProps({
    fetchFunc: newFetchFunc({
      clusterId: 'cluster1',
      search: 'foo',
      numResources: 4,
    }),
  });
  const { result, rerender } = renderHook(useKeyBasedPagination, {
    initialProps: props,
  });

  await act(result.current.fetch);
  expect(resourceClusterIds(result)).toEqual(['cluster1', 'cluster1']);
  expect(resourceNames(result)).toEqual(['foo0', 'foo1']);

  updateSearch('bar');
  await act(result.current.fetch);
  expect(resourceNames(result)).toEqual(['bar0', 'bar1']);

  // Make sure we reached the end of the data set.
  await act(result.current.fetch);
  expect(result.current.finished).toBe(true);

  updateSearch('xyz');
  expect(result.current.finished).toBe(false);

  await act(result.current.fetch);
  expect(resourceNames(result)).toEqual(['xyz0', 'xyz1']);
});

test('clear() resets the state', async () => {
  const props = hookProps();
  const { result, rerender } = renderHook(useKeyBasedPagination, {
    initialProps: props,
  });
  await act(result.current.fetch);
  expect(resourceNames(result)).toEqual(['r0', 'r1']);

  act(result.current.clear);
  rerender(props);
  expect(result.current.resources).toEqual([]);
  expect(result.current.attempt.status).toBe('');
});

describe("doesn't react to fetch() calls before the previous one finishes", () => {
  let props: KeyBasedPaginationOptions<Node>, fetchSpy;

  beforeEach(() => {
    props = hookProps();
    fetchSpy = jest.spyOn(props, 'fetchFunc');
  });

  test('when called once per state reconciliation cycle', async () => {
    const { result } = renderHook(useKeyBasedPagination, {
      initialProps: props,
    });
    let f1, f2;
    act(() => {
      f1 = result.current.fetch();
    });
    act(() => {
      f2 = result.current.fetch();
    });

    await act(async () => f1);
    await act(async () => f2);
    expect(resourceNames(result)).toEqual(['r0', 'r1']);
    expect(props.fetchFunc).toHaveBeenCalledTimes(1);
  });

  test('when called multiple times per state reconciliation cycle', async () => {
    const { result } = renderHook(useKeyBasedPagination, {
      initialProps: props,
    });
    let f1, f2;
    act(() => {
      f1 = result.current.fetch();
      f2 = result.current.fetch();
    });
    await act(async () => f1);
    await act(async () => f2);
    expect(resourceNames(result)).toEqual(['r0', 'r1']);
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });
});

test('abort errors are gracefully handled', async () => {
  let props = hookProps({
    fetchFunc: newFetchFunc({
      numResources: 7,
      search: 'bar',
    }),
  });
  const { result } = renderHook(useKeyBasedPagination, {
    initialProps: props,
  });
  let fetchPromise;
  act(() => {
    fetchPromise = result.current.fetch();
  });

  // aborts the previous request
  act(() => result.current.clear());
  await act(async () => fetchPromise);

  // the abort error has been handled, data is empty
  expect(resourceNames(result)).toEqual([]);

  // fires another request
  await act(result.current.fetch);
  expect(resourceNames(result)).toEqual(['bar0', 'bar1']);
});

describe.each`
  name          | ErrorType
  ${'Error'}    | ${Error}
  ${'ApiError'} | ${ApiError}
`('for error type $name', ({ ErrorType }) => {
  it('stops fetching more pages once error is encountered', async () => {
    const props = hookProps();
    const fetchSpy = jest.spyOn(props, 'fetchFunc');
    const { result } = renderHook(useKeyBasedPagination, {
      initialProps: props,
    });

    await act(result.current.fetch);
    expect(resourceNames(result)).toEqual(['r0', 'r1']);

    fetchSpy.mockImplementationOnce(async () => {
      throw new ErrorType('OMGOMG');
    });
    await act(result.current.fetch);
    expect(result.current.attempt.status).toBe('failed');
    expect(resourceNames(result)).toEqual(['r0', 'r1']);

    await act(result.current.fetch);
    expect(result.current.attempt.status).toBe('failed');
    expect(resourceNames(result)).toEqual(['r0', 'r1']);
  });

  it('restarts fetching after error if fetch function changes', async () => {
    const updateSearch = (search: string) => {
      act(() => result.current.clear());
      rerender({
        ...props,
        fetchFunc: newFetchFunc({
          search,
          numResources: 7,
        }),
      });
    };
    let props = hookProps({
      fetchFunc: newFetchFunc({
        search: 'foo',
        numResources: 7,
      }),
    });
    const fetchSpy = jest.spyOn(props, 'fetchFunc');

    const { result, rerender } = renderHook(useKeyBasedPagination, {
      initialProps: props,
    });
    await act(result.current.fetch);
    expect(resourceNames(result)).toEqual(['foo0', 'foo1']);

    fetchSpy.mockImplementationOnce(async () => {
      throw new ErrorType('OMGOMG');
    });

    // Rerender with the same fetch function,
    // without clearing the params: noting should happen.
    rerender(props);
    await act(result.current.fetch);
    expect(resourceNames(result)).toEqual(['foo0', 'foo1']);

    // Rerender with different fetch function: expect new data to be fetched.
    updateSearch('bar');
    await act(result.current.fetch);
    expect(resourceNames(result)).toEqual(['bar0', 'bar1']);
  });

  it('resumes fetching once forceFetch is called after an error', async () => {
    const props = hookProps();
    const fetchSpy = jest.spyOn(props, 'fetchFunc');
    const { result } = renderHook(useKeyBasedPagination, {
      initialProps: props,
    });

    await act(result.current.fetch);
    fetchSpy.mockImplementationOnce(async () => {
      throw new ErrorType('OMGOMG');
    });
    await act(result.current.fetch);
    await act(() => result.current.fetch({ force: true }));

    expect(result.current.attempt.status).toBe('success');
    expect(resourceNames(result)).toEqual(['r0', 'r1', 'r2', 'r3', 'r4']);
  });
});

test('forceFetch spawns another request, even if there is one pending', async () => {
  const props = hookProps();
  const fetchSpy = jest.spyOn(props, 'fetchFunc');
  const { result } = renderHook(useKeyBasedPagination, {
    initialProps: props,
  });
  fetchSpy.mockImplementationOnce(async () => {
    return {
      agents: [
        {
          kind: 'node',
          subKind: 'teleport',
          id: 'sus',
          clusterId: 'test-cluster',
          hostname: `impostor`,
          labels: [],
          addr: '',
          tunnel: false,
          sshLogins: [],
        },
      ],
    };
  });
  let f1, f2;
  act(() => {
    f1 = result.current.fetch();
  });
  act(() => {
    f2 = result.current.fetch({ force: true });
  });
  await act(async () => Promise.all([f1, f2]));
  expect(resourceNames(result)).toEqual(['r0', 'r1']);
});

test("doesn't get confused if aborting a request still results in a successful promise", async () => {
  // This one is tricky. It turns out that somewhere in our API layer, we
  // perform some asynchronous operation that disregards the abort signal.
  // Whether it's because some platform implementation doesn't adhere to the
  // spec, or whether we miss some detail - all in all, in the principle, looks
  // like this hook can't really trust the abort signal to be 100% effective.
  let props = hookProps({
    // Create a function that will never throw an abort error.
    fetchFunc: newFetchFunc({
      search: 'rabbit',
      numResources: 1,
      newAbortError: () => null,
    }),
  });
  const { result, rerender } = renderHook(useKeyBasedPagination, {
    initialProps: props,
  });
  await act(result.current.fetch);
  expect(resourceNames(result)).toEqual(['rabbit0']);

  let f1, f2;
  props = { ...props, filter: { search: 'duck' } };
  rerender(props);
  act(() => {
    f1 = result.current.fetch();
  });

  props = { ...props, filter: { search: 'rabbit' } };
  rerender(props);
  act(() => {
    f2 = result.current.fetch();
  });

  await act(async () => Promise.all([f1, f2]));
  expect(resourceNames(result)).toEqual(['rabbit0']);
});
