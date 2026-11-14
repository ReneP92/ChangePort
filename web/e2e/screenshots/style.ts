/**
 * The sidebar is fixed to the window, so a screenshot of a whole page would
 * show it only as tall as the window, above an empty strip: while the tours
 * take one, it spans the page.
 */
export const fullHeightSidebar = `
  [data-slot="sidebar-wrapper"] { position: relative; }
  [data-slot="sidebar-container"] { position: absolute !important; height: 100% !important; }
`;
