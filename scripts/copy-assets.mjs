import { copyFileSync } from 'node:fs';

copyFileSync('node_modules/htmx.org/dist/htmx.min.js', 'web/static/dist/htmx.min.js');
copyFileSync('web/static/seller-upload.js', 'web/static/dist/seller-upload.js');
copyFileSync('web/static/category-confirm.js', 'web/static/dist/category-confirm.js');
