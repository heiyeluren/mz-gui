export type ScreenReaderContract = {
  component: string
  file: string
  role: string
  nameAttribute: string
  extras: string[]
}

export const screenReaderMatrix: ScreenReaderContract[] = [
  { component: 'ModuleRail', file: 'ModuleRail.tsx', role: 'navigation', nameAttribute: 'aria-label', extras: ['aria-current', 'aria-roledescription'] },
  { component: 'Select', file: 'Select.tsx', role: 'native-select', nameAttribute: 'aria-label', extras: ['aria-roledescription'] },
  { component: 'Checkbox', file: 'Checkbox.tsx', role: 'checkbox', nameAttribute: 'aria-label', extras: ['aria-roledescription'] },
  { component: 'Switch', file: 'Switch.tsx', role: 'switch', nameAttribute: 'aria-label', extras: ['aria-checked', 'aria-roledescription'] },
  { component: 'LiveRegion', file: 'LiveRegion.tsx', role: 'status', nameAttribute: 'aria-live', extras: ['aria-atomic', 'aria-roledescription'] },
  { component: 'Dialog', file: 'Dialog.tsx', role: 'dialog', nameAttribute: 'aria-labelledby', extras: ['aria-modal', 'trapFocus', 'aria-roledescription'] },
  { component: 'DataTable', file: 'DataTable.tsx', role: 'native-table', nameAttribute: 'caption', extras: ['aria-roledescription'] },
  { component: 'Tree', file: 'Tree.tsx', role: 'tree', nameAttribute: 'aria-label', extras: ['aria-roledescription'] },
  { component: 'Breadcrumb', file: 'Breadcrumb.tsx', role: 'navigation', nameAttribute: 'aria-label', extras: ['aria-roledescription'] },
  { component: 'ContextMenu', file: 'ContextMenu.tsx', role: 'menu', nameAttribute: 'aria-label', extras: ['aria-roledescription'] },
  { component: 'ProgressBar', file: 'ProgressBar.tsx', role: 'progressbar', nameAttribute: 'aria-label', extras: ['aria-valuenow', 'aria-roledescription'] },
  { component: 'VirtualList', file: 'VirtualList.tsx', role: 'listbox', nameAttribute: 'aria-label', extras: ['aria-roledescription'] },
  { component: 'MarkdownViewer', file: 'MarkdownViewer.tsx', role: 'article', nameAttribute: 'aria-label', extras: ['aria-roledescription'] },
  { component: 'ImageViewer', file: 'ImageViewer.tsx', role: 'img', nameAttribute: 'aria-label', extras: ['aria-roledescription'] },
  { component: 'CodeViewer', file: 'CodeViewer.tsx', role: 'region', nameAttribute: 'aria-label', extras: ['aria-roledescription'] },
  { component: 'MediaSurface', file: 'MediaSurface.tsx', role: 'status', nameAttribute: 'aria-label', extras: ['aria-roledescription'] },
  { component: 'TerminalSurface', file: 'TerminalSurface.tsx', role: 'application', nameAttribute: 'aria-label', extras: ['aria-roledescription', 'aria-live'] },
  { component: 'Tabs', file: 'Tabs.tsx', role: 'tablist', nameAttribute: 'aria-label', extras: ['aria-roledescription'] },
  { component: 'IconButton', file: 'IconButton.tsx', role: 'button', nameAttribute: 'aria-label', extras: ['aria-roledescription'] },
  { component: 'TextField', file: 'TextField.tsx', role: 'textbox', nameAttribute: 'htmlFor', extras: ['aria-invalid', 'aria-roledescription', 'aria-live'] },
  { component: 'FeedbackToast', file: 'FeedbackToast.tsx', role: 'status', nameAttribute: 'role', extras: ['alert', 'aria-roledescription'] },
  { component: 'SegmentedControl', file: 'SegmentedControl.tsx', role: 'group', nameAttribute: 'aria-label', extras: ['aria-pressed', 'aria-roledescription'] },
  { component: 'SkipToContent', file: 'SkipToContent.tsx', role: 'link', nameAttribute: 'href', extras: ['mzgui-main'] },
  { component: 'MainLandmark', file: 'SkipToContent.tsx', role: 'main', nameAttribute: 'aria-label', extras: ['id'] },
]
