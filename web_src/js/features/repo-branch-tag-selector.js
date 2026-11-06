import {createApp} from 'vue';

export async function initRepoBranchTagSelector() {
  for (const [elIndex, elRoot] of document
    .querySelectorAll('.js-branch-tag-selector')
    .entries()) {
    const {default: RepoBranchTagSelector} = await import(/* webpackChunkName: "repo-branch-tag-selector" */'../components/RepoBranchTagSelector.vue');

    createApp({
      ...RepoBranchTagSelector,
      data() {
        return {
          items: [],
          searchTerm: '',
          refNameText: '',
          menuVisible: false,
          release: null,

          isViewTag: false,
          isViewBranch: false,
          isViewTree: false,

          active: 0,
          isLoading: false,
          // This means whether branch list/tag list has initialized
          hasListInitialized: {
            branches: false,
            tags: false,
            asc: false,
          },
          // an asc repository in branchless mode says so under its menu
          ascBranchless: false,
          // the mode the dropdown falls back to when the asc menu is not available
          fallbackMode: '',
          ...window.config.pageData.branchDropdownDataList[elIndex],
        };
      },
    }).mount(elRoot);
  }
}
