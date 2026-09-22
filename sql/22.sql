-- Keep the old dashboard-template URL available for bookmarks, but remove it
-- from the visible Visualization menu after dashboard templates move to the
-- Resource Center.
UPDATE public.sys_ui_elements
SET param3 = '1'
WHERE element_code = 'visualization_thingsvis-template'
  AND param1 = '/visualization/thingsvis-template';
