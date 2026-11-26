package compose_test

import (
	"context"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
)

// A service's memory reservation comes from deploy.resources.reservations.memory,
// else mem_reservation, and its CPU reservation from
// deploy.resources.reservations.cpus. Limit keys are ignored: DCM derives
// limits from the reservation. Memory is rounded up to whole MB and CPU to the
// nearest millicore; a service without figures takes the defaults (zero).
func TestParserReadsServiceReservations(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name                 string
		service              string
		wantMemoryMB, wantMC int64
	}{
		{name: "no figures"},
		{
			name:         "deploy reservations",
			service:      "    deploy:\n      resources:\n        reservations:\n          memory: 512M\n          cpus: '0.5'\n",
			wantMemoryMB: 512, wantMC: 500,
		},
		{
			name:         "mem_reservation",
			service:      "    mem_reservation: 1g\n",
			wantMemoryMB: 1024,
		},
		{
			name:         "deploy memory wins over mem_reservation",
			service:      "    mem_reservation: 1g\n    deploy:\n      resources:\n        reservations:\n          memory: 64m\n",
			wantMemoryMB: 64,
		},
		{
			name:         "mem_reservation when deploy gives only cpus",
			service:      "    mem_reservation: 32m\n    deploy:\n      resources:\n        reservations:\n          cpus: 2\n",
			wantMemoryMB: 32, wantMC: 2000,
		},
		{
			name:    "limit keys are ignored",
			service: "    mem_limit: 2g\n    cpus: 1.5\n    deploy:\n      resources:\n        limits:\n          memory: 2g\n          cpus: '1.5'\n",
		},
		{
			name:         "memory is rounded up to whole MB",
			service:      "    mem_reservation: 20000000\n",
			wantMemoryMB: 20,
		},
		{
			name:         "a zero deploy memory takes the default over mem_reservation",
			service:      "    mem_reservation: 1g\n    deploy:\n      resources:\n        reservations:\n          memory: 0\n",
			wantMemoryMB: 0,
		},
		{
			name:         "the smallest figures",
			service:      "    deploy:\n      resources:\n        reservations:\n          memory: 16m\n          cpus: '0.01'\n",
			wantMemoryMB: 16, wantMC: 10,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yaml := "services:\n  web:\n    image: nginx\n" + tt.service

			project, err := compose.NewParser().ParseAndValidateWithBaseAndBlocked(context.Background(), "proj", []byte(yaml), nil, nil, "")

			if err != nil {
				t.Fatalf("ParseAndValidateWithBaseAndBlocked() error = %v", err)
			}
			got := project.Services[0]
			if got.MemoryReservationMB != tt.wantMemoryMB || got.CPUReservationMillicores != tt.wantMC {
				t.Fatalf("reservation = %d MB, %dm; want %d MB, %dm", got.MemoryReservationMB, got.CPUReservationMillicores, tt.wantMemoryMB, tt.wantMC)
			}
		})
	}
}

// A reservation outside the bounds fails parsing with a bad request that
// names the service and not the figure.
func TestParserRejectsAReservationOutsideTheBounds(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, service, figure, want string
	}{
		{
			name:    "deploy memory below 16 MiB",
			service: "    deploy:\n      resources:\n        reservations:\n          memory: 15m\n",
			figure:  "15",
			want:    "bad request: memory reservation for service web must be between 16 MiB and 1 TiB",
		},
		{
			name:    "mem_reservation above 1 TiB",
			service: "    mem_reservation: 1025g\n",
			figure:  "1025",
			want:    "bad request: memory reservation for service web must be between 16 MiB and 1 TiB",
		},
		{
			name:    "negative memory",
			service: "    mem_reservation: -12345678\n",
			figure:  "12345678",
			want:    "bad request: memory reservation for service web must be between 16 MiB and 1 TiB",
		},
		{
			name:    "cpus below 10 millicores",
			service: "    deploy:\n      resources:\n        reservations:\n          cpus: '0.009'\n",
			figure:  "0.009",
			want:    "bad request: cpu reservation for service web must be between 0.01 cores and the largest cpu quota",
		},
		{
			name:    "cpus that would round up to 10 millicores",
			service: "    deploy:\n      resources:\n        reservations:\n          cpus: '0.0095'\n",
			figure:  "0.0095",
			want:    "bad request: cpu reservation for service web must be between 0.01 cores and the largest cpu quota",
		},
		{
			name:    "negative cpus",
			service: "    deploy:\n      resources:\n        reservations:\n          cpus: '-3'\n",
			figure:  "3",
			want:    "bad request: cpu reservation for service web must be between 0.01 cores and the largest cpu quota",
		},
		{
			name:    "cpus above the largest cpu quota",
			service: "    deploy:\n      resources:\n        reservations:\n          cpus: '2000000'\n",
			figure:  "2000000",
			want:    "bad request: cpu reservation for service web must be between 0.01 cores and the largest cpu quota",
		},
		{
			name:    "cpus that are not a number",
			service: "    deploy:\n      resources:\n        reservations:\n          cpus: 'NaN'\n",
			figure:  "NaN",
			want:    "bad request: cpu reservation for service web must be between 0.01 cores and the largest cpu quota",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yaml := "services:\n  web:\n    image: nginx\n" + tt.service

			_, err := compose.NewParser().ParseAndValidateWithBaseAndBlocked(context.Background(), "proj", []byte(yaml), nil, nil, "")

			if got := apperrors.SafeMessage(err); strings.Contains(got, tt.figure) {
				t.Fatalf("SafeMessage() = %q echoes the figure %q", got, tt.figure)
			}
			requireRejection(t, err, apperrors.ErrBadRequest, tt.want)
		})
	}
}
