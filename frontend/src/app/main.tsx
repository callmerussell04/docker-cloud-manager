import React from 'react';
import ReactDOM from 'react-dom/client';
import { App } from '@/app/App';
import '@fontsource-variable/inter/opsz.css';
import '@fontsource-variable/geist-mono';
import './index.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);