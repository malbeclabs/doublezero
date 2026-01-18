package isis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Run("valid dump", func(t *testing.T) {
		data := []byte(`{
			"vrfs": {
				"default": {
					"isisInstances": {
						"1": {
							"level": {
								"2": {
									"lsps": {
										"ac10.0001.0000.00-00": {
											"hostname": {"name": "DZ-NY7-SW01"},
											"routerCapabilities": [{
												"routerId": "172.16.0.1",
												"srgbBase": 16000,
												"srgbRange": 8000
											}],
											"neighbors": [
												{
													"systemId": "ac10.0002.0000",
													"metric": 1000,
													"neighborAddr": "172.16.0.117",
													"adjSids": [{"adjSid": 100001}, {"adjSid": 100002}]
												},
												{
													"systemId": "ac10.0003.0000",
													"metric": 2000,
													"neighborAddr": "172.16.0.119",
													"adjSids": [{"adjSid": 100003}]
												}
											]
										},
										"ac10.0002.0000.00-00": {
											"hostname": {"name": "DZ-DC1-SW01"},
											"routerCapabilities": [{
												"routerId": "172.16.0.2",
												"srgbBase": 16000,
												"srgbRange": 8000
											}],
											"neighbors": [
												{
													"systemId": "ac10.0001.0000",
													"metric": 1000,
													"neighborAddr": "172.16.0.116",
													"adjSids": [{"adjSid": 100001}]
												}
											]
										}
									}
								}
							}
						}
					}
				}
			}
		}`)

		lsps, err := Parse(data)
		require.NoError(t, err)
		assert.Len(t, lsps, 2)

		// Find the LSP for NY7
		var ny7LSP *LSP
		for i := range lsps {
			if lsps[i].Hostname == "DZ-NY7-SW01" {
				ny7LSP = &lsps[i]
				break
			}
		}
		require.NotNil(t, ny7LSP, "expected to find DZ-NY7-SW01 LSP")

		assert.Equal(t, "ac10.0001.0000.00-00", ny7LSP.SystemID)
		assert.Equal(t, "DZ-NY7-SW01", ny7LSP.Hostname)
		assert.Equal(t, "172.16.0.1", ny7LSP.RouterID)
		assert.Len(t, ny7LSP.Neighbors, 2)

		// Check first neighbor
		assert.Equal(t, "ac10.0002.0000", ny7LSP.Neighbors[0].SystemID)
		assert.Equal(t, uint32(1000), ny7LSP.Neighbors[0].Metric)
		assert.Equal(t, "172.16.0.117", ny7LSP.Neighbors[0].NeighborAddr)
		assert.Equal(t, []uint32{100001, 100002}, ny7LSP.Neighbors[0].AdjSIDs)
	})

	t.Run("empty neighbors", func(t *testing.T) {
		data := []byte(`{
			"vrfs": {
				"default": {
					"isisInstances": {
						"1": {
							"level": {
								"2": {
									"lsps": {
										"ac10.0001.0000.00-00": {
											"hostname": {"name": "DZ-NY7-SW01"},
											"routerCapabilities": [{"routerId": "172.16.0.1"}],
											"neighbors": []
										}
									}
								}
							}
						}
					}
				}
			}
		}`)

		lsps, err := Parse(data)
		require.NoError(t, err)
		assert.Len(t, lsps, 1)
		assert.Empty(t, lsps[0].Neighbors)
	})

	t.Run("missing VRF default", func(t *testing.T) {
		data := []byte(`{
			"vrfs": {
				"other": {}
			}
		}`)

		_, err := Parse(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VRF 'default' not found")
	})

	t.Run("missing ISIS instance", func(t *testing.T) {
		data := []byte(`{
			"vrfs": {
				"default": {
					"isisInstances": {}
				}
			}
		}`)

		_, err := Parse(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "IS-IS instance '1' not found")
	})

	t.Run("missing level 2", func(t *testing.T) {
		data := []byte(`{
			"vrfs": {
				"default": {
					"isisInstances": {
						"1": {
							"level": {}
						}
					}
				}
			}
		}`)

		_, err := Parse(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "IS-IS level '2' not found")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		data := []byte(`{invalid}`)

		_, err := Parse(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal JSON")
	})

	t.Run("null adjSids", func(t *testing.T) {
		data := []byte(`{
			"vrfs": {
				"default": {
					"isisInstances": {
						"1": {
							"level": {
								"2": {
									"lsps": {
										"ac10.0001.0000.00-00": {
											"hostname": {"name": "DZ-NY7-SW01"},
											"routerCapabilities": [{"routerId": "172.16.0.1"}],
											"neighbors": [
												{
													"systemId": "ac10.0002.0000",
													"metric": 1000,
													"neighborAddr": "172.16.0.117",
													"adjSids": null
												}
											]
										}
									}
								}
							}
						}
					}
				}
			}
		}`)

		lsps, err := Parse(data)
		require.NoError(t, err)
		assert.Len(t, lsps, 1)
		assert.Nil(t, lsps[0].Neighbors[0].AdjSIDs)
	})
}
