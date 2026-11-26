import { useNavigate } from 'react-router-dom';
import { newContainerPath } from '@/features/containers/model';
import { ImagesPage } from '@/features/images/pages/ImagesPage';

// ImagesRoute opens an image's Run a container in the containers feature's
// create dialog. The images feature may not import the containers feature,
// so the app layer joins them.
export function ImagesRoute() {
  const navigate = useNavigate();
  return <ImagesPage onRun={(image) => navigate(newContainerPath(image.tag))} />;
}
