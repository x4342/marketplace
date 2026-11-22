import { copyFileSync } from 'node:fs';

copyFileSync('node_modules/htmx.org/dist/htmx.min.js', 'web/static/dist/htmx.min.js');
