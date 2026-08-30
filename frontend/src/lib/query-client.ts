import { QueryClient } from '@tanstack/svelte-query';
import { keepPreviousData } from '@tanstack/query-core';

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      retry: 1,
      refetchOnWindowFocus: false,
      refetchIntervalInBackground: false,
      placeholderData: keepPreviousData,
    },
  },
});
