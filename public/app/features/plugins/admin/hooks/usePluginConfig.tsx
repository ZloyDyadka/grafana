import { useAsync } from 'react-use';

import { CatalogPlugin } from '../../types';
import { loadPlugin } from '../../utils';

export const usePluginConfig = (plugin?: CatalogPlugin) => {
  return useAsync(async () => {
    if (!plugin) {
      return null;
    }

    if (plugin.settings.isInstalled && !plugin.settings.isDisabled) {
      return loadPlugin(plugin.id);
    }

    return null;
  }, [plugin?.id, plugin?.settings.isInstalled, plugin?.settings.isDisabled]);
};
